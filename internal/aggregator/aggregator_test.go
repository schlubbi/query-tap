package aggregator

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/schlubbi/query-tap/internal/comment"
	"github.com/schlubbi/query-tap/internal/fingerprint"
)

func newTestAggregator(parser comment.CommentParser) *Aggregator {
	fp := fingerprint.New(1000)
	return New(fp, parser)
}

func TestRecordQuery_100SameFingerprint(t *testing.T) {
	agg := newTestAggregator(nil)
	now := time.Now()

	for i := range 100 {
		q := fmt.Sprintf("SELECT * FROM users WHERE id = %d", i)
		agg.RecordQuery(q, uint64(1000+i), now.Add(time.Duration(i)*time.Millisecond))
	}

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}

	s := snap[0]
	if s.Count != 100 {
		t.Errorf("expected count=100, got %d", s.Count)
	}
	if s.FingerprintID == "" {
		t.Error("expected non-empty FingerprintID")
	}
	if s.Fingerprint == "" {
		t.Error("expected non-empty Fingerprint (normalized query)")
	}
	// HDR histogram should have values — p50 should be somewhere in recorded range.
	if s.P50Ns == 0 {
		t.Error("expected non-zero P50Ns")
	}
}

func TestRecordQuery_DifferentFingerprints(t *testing.T) {
	agg := newTestAggregator(nil)
	now := time.Now()

	agg.RecordQuery("SELECT * FROM users WHERE id = 1", 1000, now)
	agg.RecordQuery("SELECT * FROM orders WHERE user_id = 2", 2000, now)
	agg.RecordQuery("INSERT INTO logs VALUES (1, 'test')", 3000, now)

	snap := agg.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected 3 fingerprints, got %d", len(snap))
	}

	ids := make(map[string]bool, len(snap))
	for _, s := range snap {
		if ids[s.FingerprintID] {
			t.Errorf("duplicate FingerprintID %q", s.FingerprintID)
		}
		ids[s.FingerprintID] = true
		if s.Count != 1 {
			t.Errorf("expected count=1 for fingerprint %q, got %d", s.FingerprintID, s.Count)
		}
	}
}

func TestPercentiles_KnownLatencies(t *testing.T) {
	agg := newTestAggregator(nil)
	now := time.Now()

	// Record 100 queries with latencies 1ms, 2ms, ..., 100ms.
	for i := 1; i <= 100; i++ {
		latency := uint64(i) * 1_000_000 // i ms in nanoseconds
		agg.RecordQuery(fmt.Sprintf("SELECT * FROM t WHERE id = %d", i), latency, now)
	}

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}
	s := snap[0]

	// With 100 values 1ms..100ms:
	// p50 ≈ 50ms, p95 ≈ 95ms, p99 ≈ 99ms
	// HDR histograms have some precision loss, so use a tolerance.
	assertPercentileInRange(t, "P50", s.P50Ns, 48_000_000, 52_000_000)
	assertPercentileInRange(t, "P95", s.P95Ns, 93_000_000, 97_000_000)
	assertPercentileInRange(t, "P99", s.P99Ns, 97_000_000, 101_000_000)
}

func assertPercentileInRange(t *testing.T, name string, got int64, low, high int64) {
	t.Helper()
	if got < low || got > high {
		t.Errorf("%s = %d ns, expected in range [%d, %d]", name, got, low, high)
	}
}

func TestSnapshot_SortedByTotalLatencyDescending(t *testing.T) {
	agg := newTestAggregator(nil)
	now := time.Now()

	// 3 fingerprints with different total latencies.
	// "users" gets 1 query at 100ns = total 100ns
	agg.RecordQuery("SELECT * FROM users WHERE id = 1", 100, now)
	// "orders" gets 1 query at 300ns = total 300ns
	agg.RecordQuery("SELECT * FROM orders WHERE id = 1", 300, now)
	// "logs" gets 1 query at 200ns = total 200ns
	agg.RecordQuery("SELECT * FROM logs WHERE id = 1", 200, now)

	snap := agg.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected 3 fingerprints, got %d", len(snap))
	}

	if snap[0].TotalNs != 300 {
		t.Errorf("expected first entry TotalNs=300, got %d", snap[0].TotalNs)
	}
	if snap[1].TotalNs != 200 {
		t.Errorf("expected second entry TotalNs=200, got %d", snap[1].TotalNs)
	}
	if snap[2].TotalNs != 100 {
		t.Errorf("expected third entry TotalNs=100, got %d", snap[2].TotalNs)
	}
}

func TestTags_AttachedFromCommentParser(t *testing.T) {
	parser := &comment.MarginaliaParser{}
	agg := newTestAggregator(parser)
	now := time.Now()

	agg.RecordQuery("/* app=web,controller=users */ SELECT * FROM users WHERE id = 1", 1000, now)

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}
	s := snap[0]

	if s.Tags == nil {
		t.Fatal("expected non-nil Tags")
	}
	if s.Tags["app"] != "web" {
		t.Errorf("expected Tags[app]=web, got %q", s.Tags["app"])
	}
	if s.Tags["controller"] != "users" {
		t.Errorf("expected Tags[controller]=users, got %q", s.Tags["controller"])
	}
}

func TestTags_MostRecentWins(t *testing.T) {
	parser := &comment.MarginaliaParser{}
	agg := newTestAggregator(parser)
	now := time.Now()

	// First query with tag app=web.
	agg.RecordQuery("/* app=web */ SELECT * FROM users WHERE id = 1", 1000, now)
	// Second query (same fingerprint) with tag app=api.
	agg.RecordQuery("/* app=api */ SELECT * FROM users WHERE id = 2", 1000, now.Add(time.Second))

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}
	if snap[0].Tags["app"] != "api" {
		t.Errorf("expected Tags[app]=api (most recent), got %q", snap[0].Tags["app"])
	}
}

func TestQPS_Computation(t *testing.T) {
	fp := fingerprint.New(1000)
	agg := New(fp, nil)

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Record 10 queries over 10 seconds (1 per second).
	for i := range 10 {
		agg.RecordQuery(
			fmt.Sprintf("SELECT * FROM t WHERE id = %d", i),
			1000,
			start.Add(time.Duration(i)*time.Second),
		)
	}

	// QPS is computed as count / elapsed_since_window_start.
	// windowStart = start, so elapsed = time.Since(start) which is large.
	// To get a deterministic test, verify QPS > 0 and that count/totalEvents are correct.
	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}
	if snap[0].QPS <= 0 {
		t.Error("expected QPS > 0")
	}
	if snap[0].Count != 10 {
		t.Errorf("expected count=10, got %d", snap[0].Count)
	}
}

func TestRecordQuery_EmptyQuery_NoPanic(t *testing.T) {
	agg := newTestAggregator(nil)
	now := time.Now()

	// Should not panic.
	agg.RecordQuery("", 1000, now)

	snap := agg.Snapshot()
	if len(snap) != 0 {
		t.Errorf("expected 0 fingerprints for empty query, got %d", len(snap))
	}

	stats := agg.Stats()
	if stats.TotalEvents != 0 {
		t.Errorf("expected 0 total events for empty query, got %d", stats.TotalEvents)
	}
}

func TestRecordQuery_NoComment_TagsEmpty(t *testing.T) {
	parser := &comment.MarginaliaParser{}
	agg := newTestAggregator(parser)
	now := time.Now()

	agg.RecordQuery("SELECT * FROM users WHERE id = 1", 1000, now)

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}
	// Tags should be nil or empty map when no comment present.
	if len(snap[0].Tags) != 0 {
		t.Errorf("expected empty Tags for query without comment, got %v", snap[0].Tags)
	}
}

func TestStats_ReturnsCorrectTotals(t *testing.T) {
	agg := newTestAggregator(nil)
	now := time.Now()

	agg.RecordQuery("SELECT * FROM users WHERE id = 1", 1000, now)
	agg.RecordQuery("SELECT * FROM orders WHERE id = 1", 2000, now)
	agg.RecordQuery("SELECT * FROM users WHERE id = 2", 3000, now) // same fingerprint as first

	stats := agg.Stats()
	if stats.TotalEvents != 3 {
		t.Errorf("expected TotalEvents=3, got %d", stats.TotalEvents)
	}
	if stats.ActiveFingerprints != 2 {
		t.Errorf("expected ActiveFingerprints=2, got %d", stats.ActiveFingerprints)
	}
}

func TestMinMaxTracking(t *testing.T) {
	agg := newTestAggregator(nil)
	now := time.Now()

	latencies := []uint64{5000, 1000, 9000, 3000, 7000}
	for i, lat := range latencies {
		agg.RecordQuery(fmt.Sprintf("SELECT * FROM t WHERE id = %d", i), lat, now)
	}

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}
	if snap[0].MinNs != 1000 {
		t.Errorf("expected MinNs=1000, got %d", snap[0].MinNs)
	}
	if snap[0].MaxNs != 9000 {
		t.Errorf("expected MaxNs=9000, got %d", snap[0].MaxNs)
	}
}

func TestTotalLatencyAccumulation(t *testing.T) {
	agg := newTestAggregator(nil)
	now := time.Now()

	agg.RecordQuery("SELECT * FROM t WHERE id = 1", 100, now)
	agg.RecordQuery("SELECT * FROM t WHERE id = 2", 200, now)
	agg.RecordQuery("SELECT * FROM t WHERE id = 3", 300, now)

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}
	if snap[0].TotalNs != 600 {
		t.Errorf("expected TotalNs=600, got %d", snap[0].TotalNs)
	}
}

func TestSnapshot_CopiesData(t *testing.T) {
	parser := &comment.MarginaliaParser{}
	agg := newTestAggregator(parser)
	now := time.Now()

	agg.RecordQuery("/* app=web */ SELECT * FROM t WHERE id = 1", 1000, now)

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}

	// Mutate the snapshot — should not affect internal state.
	snap[0].Tags["app"] = "mutated"
	snap[0].Count = 999

	snap2 := agg.Snapshot()
	if snap2[0].Tags["app"] != "web" {
		t.Errorf("internal state leaked: Tags[app] = %q, want %q", snap2[0].Tags["app"], "web")
	}
	if snap2[0].Count != 1 {
		t.Errorf("internal state leaked: Count = %d, want 1", snap2[0].Count)
	}
}

func TestDigestFieldsPropagated(t *testing.T) {
	fp := fingerprint.New(1000)
	agg := New(fp, nil)
	now := time.Now()

	agg.RecordQuery("SELECT * FROM users WHERE id = 1", 1000, now)

	// Resolve digest on the fingerprinter.
	entry := fp.Fingerprint("SELECT * FROM users WHERE id = 2")
	fp.SetDigest(entry.ID, "abc123hash", "SELECT * FROM users WHERE id = ?")

	// Record another query to trigger digest field refresh.
	agg.RecordQuery("SELECT * FROM users WHERE id = 3", 2000, now)

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}
	if snap[0].DigestHash != "abc123hash" {
		t.Errorf("expected DigestHash=abc123hash, got %q", snap[0].DigestHash)
	}
	if snap[0].DigestText != "SELECT * FROM users WHERE id = ?" {
		t.Errorf("expected DigestText='SELECT * FROM users WHERE id = ?', got %q", snap[0].DigestText)
	}
}

func TestNilParser_NoTags(t *testing.T) {
	agg := newTestAggregator(nil)
	now := time.Now()

	agg.RecordQuery("/* app=web */ SELECT * FROM t WHERE id = 1", 1000, now)

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}
	if len(snap[0].Tags) != 0 {
		t.Errorf("expected no tags with nil parser, got %v", snap[0].Tags)
	}
}

func TestConcurrentRecordAndSnapshot(t *testing.T) {
	agg := newTestAggregator(nil)
	now := time.Now()

	var wg sync.WaitGroup
	// Writers.
	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			q := fmt.Sprintf("SELECT * FROM t WHERE id = %d", idx)
			agg.RecordQuery(q, uint64(1000+idx), now.Add(time.Duration(idx)*time.Millisecond))
		}(i)
	}
	// Concurrent readers.
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = agg.Snapshot()
			_ = agg.Stats()
		}()
	}

	wg.Wait()

	stats := agg.Stats()
	if stats.TotalEvents != 100 {
		t.Errorf("expected TotalEvents=100, got %d", stats.TotalEvents)
	}
}

func TestLastSeenTracking(t *testing.T) {
	agg := newTestAggregator(nil)

	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 1, 0, 0, 5, 0, time.UTC)

	agg.RecordQuery("SELECT * FROM t WHERE id = 1", 1000, t1)
	agg.RecordQuery("SELECT * FROM t WHERE id = 2", 2000, t2)

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}
	if !snap[0].LastSeen.Equal(t2) {
		t.Errorf("expected LastSeen=%v, got %v", t2, snap[0].LastSeen)
	}
}

func TestMinNs_InitializedCorrectly(t *testing.T) {
	agg := newTestAggregator(nil)
	now := time.Now()

	// First query should set MinNs, not leave it at 0 or MaxUint64.
	agg.RecordQuery("SELECT * FROM t WHERE id = 1", 5000, now)

	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 fingerprint, got %d", len(snap))
	}
	if snap[0].MinNs != 5000 {
		t.Errorf("expected MinNs=5000 after first query, got %d", snap[0].MinNs)
	}
	if snap[0].MinNs == math.MaxUint64 {
		t.Error("MinNs should not be MaxUint64 after recording a query")
	}
}
