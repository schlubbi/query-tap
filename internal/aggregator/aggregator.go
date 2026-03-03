package aggregator

import (
	"math"
	"sort"
	"sync"
	"time"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"
	"github.com/schlubbi/query-tap/internal/comment"
	"github.com/schlubbi/query-tap/internal/fingerprint"
)

const (
	// histMinNs is the minimum recordable latency (1 ns).
	histMinNs = 1
	// histMaxNs is the maximum recordable latency (60 seconds).
	histMaxNs = 60_000_000_000
	// histSigFigs is the number of significant figures for HDR histograms.
	histSigFigs = 3
)

// fingerprintState holds internal per-fingerprint state including the HDR histogram.
type fingerprintState struct {
	fingerprintID string
	normalized    string
	digestHash    string
	digestText    string
	tags          map[string]string
	sampleQuery   string
	count         uint64
	totalNs       uint64
	minNs         uint64
	maxNs         uint64
	lastSeen      time.Time
	histogram     *hdrhistogram.Histogram
}

// Aggregator combines fingerprinting, comment parsing, and per-fingerprint stats.
type Aggregator struct {
	mu            sync.Mutex
	fingerprinter *fingerprint.Fingerprinter
	parser        comment.CommentParser
	entries       map[string]*fingerprintState
	totalEvents   uint64
	windowStart   time.Time
}

// New creates an Aggregator that uses the given fingerprinter and comment parser.
// The parser may be nil, in which case comment tags are not extracted.
func New(fingerprinter *fingerprint.Fingerprinter, parser comment.CommentParser) *Aggregator {
	return &Aggregator{
		fingerprinter: fingerprinter,
		parser:        parser,
		entries:       make(map[string]*fingerprintState),
	}
}

// RecordQuery processes a raw query with its latency and updates per-fingerprint stats.
//
// Pipeline: raw SQL → extract comment → parse tags → fingerprint → update stats.
func (a *Aggregator) RecordQuery(query string, latencyNs uint64, timestamp time.Time) {
	if query == "" {
		return
	}

	// Extract comment and parse tags.
	var tags map[string]string
	if a.parser != nil {
		body, _ := comment.ExtractComment(query)
		if body != "" {
			tags = a.parser.Parse(body)
		}
	}

	// Fingerprint (the fingerprinter strips comments internally).
	entry := a.fingerprinter.Fingerprint(query)

	a.mu.Lock()
	defer a.mu.Unlock()

	// Set window start on first event.
	if a.totalEvents == 0 {
		a.windowStart = timestamp
	}
	a.totalEvents++

	state, ok := a.entries[entry.ID]
	if !ok {
		state = &fingerprintState{
			fingerprintID: entry.ID,
			normalized:    entry.Normalized,
			sampleQuery:   entry.SampleQuery,
			minNs:         math.MaxUint64,
			histogram:     hdrhistogram.New(histMinNs, histMaxNs, histSigFigs),
		}
		a.entries[entry.ID] = state
	}

	// Update digest fields from fingerprinter (may have been resolved since last record).
	state.digestHash = entry.DigestHash
	state.digestText = entry.DigestText

	// Update tags — most recent tags win.
	if len(tags) > 0 {
		state.tags = tags
	}

	state.count++
	state.totalNs += latencyNs
	if latencyNs < state.minNs {
		state.minNs = latencyNs
	}
	if latencyNs > state.maxNs {
		state.maxNs = latencyNs
	}
	state.lastSeen = timestamp

	// Clamp latency to histogram range before recording.
	clamped := int64(latencyNs)
	if clamped < histMinNs {
		clamped = histMinNs
	}
	if clamped > histMaxNs {
		clamped = histMaxNs
	}
	_ = state.histogram.RecordValue(clamped)
}

// Snapshot returns current stats for all fingerprints, sorted by total latency descending.
// The returned slice is a copy — callers hold no references to internal state.
func (a *Aggregator) Snapshot() []FingerprintStats {
	a.mu.Lock()
	defer a.mu.Unlock()

	elapsed := time.Since(a.windowStart).Seconds()
	if a.totalEvents == 0 {
		elapsed = 0
	}

	out := make([]FingerprintStats, 0, len(a.entries))
	for _, s := range a.entries {
		var qps float64
		if elapsed > 0 {
			qps = float64(s.count) / elapsed
		}

		// Copy tags.
		var tagsCopy map[string]string
		if len(s.tags) > 0 {
			tagsCopy = make(map[string]string, len(s.tags))
			for k, v := range s.tags {
				tagsCopy[k] = v
			}
		}

		out = append(out, FingerprintStats{
			FingerprintID: s.fingerprintID,
			Fingerprint:   s.normalized,
			DigestHash:    s.digestHash,
			DigestText:    s.digestText,
			Tags:          tagsCopy,
			SampleQuery:   s.sampleQuery,
			Count:         s.count,
			TotalNs:       s.totalNs,
			MinNs:         s.minNs,
			MaxNs:         s.maxNs,
			P50Ns:         s.histogram.ValueAtQuantile(50),
			P95Ns:         s.histogram.ValueAtQuantile(95),
			P99Ns:         s.histogram.ValueAtQuantile(99),
			LastSeen:      s.lastSeen,
			QPS:           qps,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].TotalNs > out[j].TotalNs
	})

	return out
}

// Stats returns operational metrics about the aggregator.
func (a *Aggregator) Stats() AggregatorStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AggregatorStats{
		TotalEvents:        a.totalEvents,
		ActiveFingerprints: len(a.entries),
		Evictions:          a.fingerprinter.Evictions(),
	}
}
