// Package pipeline correlates query and latency events from the BPF ring
// buffer and feeds matched pairs to the aggregator.
package pipeline

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/schlubbi/query-tap/internal/ebpf"
)

// QueryRecorder is implemented by types that record matched query/latency pairs.
// *aggregator.Aggregator satisfies this interface.
type QueryRecorder interface {
	RecordQuery(query string, latencyNs uint64, timestamp time.Time)
}

// Pipeline correlates query and latency events, feeds the aggregator.
type Pipeline struct {
	recorder QueryRecorder
	mu       sync.Mutex
	pending  map[uint32]*pendingQuery // tid → pending query (waiting for latency)
	logger   *slog.Logger
}

type pendingQuery struct {
	query     string
	timestamp time.Time
}

// New creates a Pipeline that sends matched events to the given recorder.
func New(recorder QueryRecorder, logger *slog.Logger) *Pipeline {
	return &Pipeline{
		recorder: recorder,
		pending:  make(map[uint32]*pendingQuery),
		logger:   logger,
	}
}

// ProcessEvent handles a single event from the BPF ring buffer.
// QueryEvents are buffered; LatencyEvents are matched to their query by TID.
func (p *Pipeline) ProcessEvent(evt ebpf.Event) {
	switch e := evt.(type) {
	case *ebpf.QueryEvent:
		p.mu.Lock()
		p.pending[e.TID] = &pendingQuery{
			query:     e.QueryString(),
			timestamp: time.Now(),
		}
		p.mu.Unlock()

	case *ebpf.LatencyEvent:
		p.mu.Lock()
		pq, ok := p.pending[e.TID]
		if !ok {
			p.mu.Unlock()
			p.logger.Debug("orphan latency event (no matching query)", "tid", e.TID)
			return
		}
		delete(p.pending, e.TID)
		p.mu.Unlock()

		p.recorder.RecordQuery(pq.query, e.LatencyNs, pq.timestamp)
	}
}

// Run reads events from a channel and processes them.
// Blocks until ctx is cancelled or the channel is closed.
func (p *Pipeline) Run(ctx context.Context, events <-chan ebpf.Event) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-events:
			if !ok {
				return nil
			}
			p.ProcessEvent(evt)
		}
	}
}

// PendingCount returns the number of query events awaiting a matching latency.
func (p *Pipeline) PendingCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pending)
}
