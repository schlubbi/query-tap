package pipeline

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/schlubbi/query-tap/internal/aggregator"
	"github.com/schlubbi/query-tap/internal/ebpf"
	"github.com/schlubbi/query-tap/internal/fingerprint"
)

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discard{}, nil))
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// recordedQuery captures a single call to Aggregator.RecordQuery for assertions.
type recordedQuery struct {
	query     string
	latencyNs uint64
	timestamp time.Time
}

// spyAggregator wraps a real Aggregator to record calls to RecordQuery.
type spyAggregator struct {
	*aggregator.Aggregator
	mu      sync.Mutex
	calls   []recordedQuery
}

func newSpyAggregator() *spyAggregator {
	fp := fingerprint.New(1000)
	return &spyAggregator{
		Aggregator: aggregator.New(fp, nil),
	}
}

func (s *spyAggregator) RecordQuery(query string, latencyNs uint64, timestamp time.Time) {
	s.mu.Lock()
	s.calls = append(s.calls, recordedQuery{query: query, latencyNs: latencyNs, timestamp: timestamp})
	s.mu.Unlock()
	s.Aggregator.RecordQuery(query, latencyNs, timestamp)
}

func (s *spyAggregator) getCalls() []recordedQuery {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedQuery, len(s.calls))
	copy(out, s.calls)
	return out
}

func TestProcessEvent_QueryThenLatency_RecordsQuery(t *testing.T) {
	spy := newSpyAggregator()
	p := New(spy, noopLogger())

	tid := uint32(42)
	query := "SELECT * FROM users WHERE id = 1"
	latencyNs := uint64(5_000_000)

	p.ProcessEvent(&ebpf.QueryEvent{
		TimestampNs: 100,
		TID:         tid,
		Command:     3,
		QueryLen:    uint16(len(query)),
		Query:       toQueryBuf(query),
	})

	p.ProcessEvent(&ebpf.LatencyEvent{
		TimestampNs: 200,
		TID:         tid,
		LatencyNs:   latencyNs,
	})

	calls := spy.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 RecordQuery call, got %d", len(calls))
	}
	if calls[0].query != query {
		t.Errorf("expected query %q, got %q", query, calls[0].query)
	}
	if calls[0].latencyNs != latencyNs {
		t.Errorf("expected latencyNs=%d, got %d", latencyNs, calls[0].latencyNs)
	}
}

func TestProcessEvent_OrphanLatency_NoCrash(t *testing.T) {
	spy := newSpyAggregator()
	p := New(spy, noopLogger())

	// Send a latency event with no preceding query event.
	p.ProcessEvent(&ebpf.LatencyEvent{
		TimestampNs: 200,
		TID:         99,
		LatencyNs:   1_000_000,
	})

	calls := spy.getCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 RecordQuery calls for orphan latency, got %d", len(calls))
	}
}

func TestProcessEvent_QueryWithoutLatency_StaysPending(t *testing.T) {
	spy := newSpyAggregator()
	p := New(spy, noopLogger())

	tid := uint32(42)
	query := "SELECT 1"

	p.ProcessEvent(&ebpf.QueryEvent{
		TimestampNs: 100,
		TID:         tid,
		Command:     3,
		QueryLen:    uint16(len(query)),
		Query:       toQueryBuf(query),
	})

	calls := spy.getCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 RecordQuery calls before latency arrives, got %d", len(calls))
	}

	if p.PendingCount() != 1 {
		t.Errorf("expected 1 pending query, got %d", p.PendingCount())
	}
}

func TestProcessEvent_MultiplePairs_AllProcessed(t *testing.T) {
	spy := newSpyAggregator()
	p := New(spy, noopLogger())

	for tid := uint32(1); tid <= 5; tid++ {
		query := "SELECT * FROM t WHERE id = ?"
		p.ProcessEvent(&ebpf.QueryEvent{
			TimestampNs: uint64(tid) * 100,
			TID:         tid,
			Command:     3,
			QueryLen:    uint16(len(query)),
			Query:       toQueryBuf(query),
		})
		p.ProcessEvent(&ebpf.LatencyEvent{
			TimestampNs: uint64(tid)*100 + 50,
			TID:         tid,
			LatencyNs:   uint64(tid) * 1_000_000,
		})
	}

	calls := spy.getCalls()
	if len(calls) != 5 {
		t.Fatalf("expected 5 RecordQuery calls, got %d", len(calls))
	}

	for i, c := range calls {
		expectedLatency := uint64(i+1) * 1_000_000
		if c.latencyNs != expectedLatency {
			t.Errorf("call[%d]: expected latencyNs=%d, got %d", i, expectedLatency, c.latencyNs)
		}
	}

	if p.PendingCount() != 0 {
		t.Errorf("expected 0 pending queries after all pairs matched, got %d", p.PendingCount())
	}
}

func TestProcessEvent_SameTID_OverwritesPending(t *testing.T) {
	spy := newSpyAggregator()
	p := New(spy, noopLogger())

	tid := uint32(42)

	// First query for this TID — will be overwritten.
	p.ProcessEvent(&ebpf.QueryEvent{
		TimestampNs: 100,
		TID:         tid,
		Command:     3,
		QueryLen:    uint16(len("SELECT 1")),
		Query:       toQueryBuf("SELECT 1"),
	})

	// Second query for same TID before latency arrives — overwrites first.
	secondQuery := "SELECT 2"
	p.ProcessEvent(&ebpf.QueryEvent{
		TimestampNs: 200,
		TID:         tid,
		Command:     3,
		QueryLen:    uint16(len(secondQuery)),
		Query:       toQueryBuf(secondQuery),
	})

	p.ProcessEvent(&ebpf.LatencyEvent{
		TimestampNs: 300,
		TID:         tid,
		LatencyNs:   1_000,
	})

	calls := spy.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 RecordQuery call, got %d", len(calls))
	}
	if calls[0].query != secondQuery {
		t.Errorf("expected query %q (most recent), got %q", secondQuery, calls[0].query)
	}
}

func TestRun_ContextCancellation_StopsCleanly(t *testing.T) {
	spy := newSpyAggregator()
	p := New(spy, noopLogger())

	ctx, cancel := context.WithCancel(context.Background())
	// Buffered so sends don't block if Run exits early.
	events := make(chan ebpf.Event, 10)

	query := "SELECT 1"
	events <- &ebpf.QueryEvent{
		TimestampNs: 100,
		TID:         1,
		Command:     3,
		QueryLen:    uint16(len(query)),
		Query:       toQueryBuf(query),
	}
	events <- &ebpf.LatencyEvent{
		TimestampNs: 200,
		TID:         1,
		LatencyNs:   500,
	}

	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx, events)
	}()

	// Give Run a moment to process buffered events, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop within 2 seconds after context cancellation")
	}

	calls := spy.getCalls()
	if len(calls) != 1 {
		t.Errorf("expected 1 RecordQuery call, got %d", len(calls))
	}
}

func TestRun_ChannelClose_StopsCleanly(t *testing.T) {
	spy := newSpyAggregator()
	p := New(spy, noopLogger())

	ctx := context.Background()
	events := make(chan ebpf.Event)

	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx, events)
	}()

	close(events)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop within 2 seconds after channel close")
	}
}

func TestProcessEvent_ConcurrentProcessing_NoRace(t *testing.T) {
	spy := newSpyAggregator()
	p := New(spy, noopLogger())

	var wg sync.WaitGroup
	for tid := uint32(1); tid <= 50; tid++ {
		wg.Add(1)
		go func(tid uint32) {
			defer wg.Done()
			query := "SELECT * FROM t WHERE id = ?"
			p.ProcessEvent(&ebpf.QueryEvent{
				TimestampNs: uint64(tid) * 100,
				TID:         tid,
				Command:     3,
				QueryLen:    uint16(len(query)),
				Query:       toQueryBuf(query),
			})
			p.ProcessEvent(&ebpf.LatencyEvent{
				TimestampNs: uint64(tid)*100 + 50,
				TID:         tid,
				LatencyNs:   uint64(tid) * 1_000,
			})
		}(tid)
	}

	wg.Wait()

	calls := spy.getCalls()
	if len(calls) != 50 {
		t.Errorf("expected 50 RecordQuery calls, got %d", len(calls))
	}
	if p.PendingCount() != 0 {
		t.Errorf("expected 0 pending after all pairs matched, got %d", p.PendingCount())
	}
}

// toQueryBuf converts a string to the fixed-size [4096]byte query buffer.
func toQueryBuf(s string) [4096]byte {
	var buf [4096]byte
	copy(buf[:], s)
	return buf
}
