// Package aggregator provides per-fingerprint metrics aggregation (count, latency percentiles).
package aggregator

import "time"

// FingerprintStats holds per-fingerprint metrics exported as a snapshot.
// Percentile fields (P50Ns, P95Ns, P99Ns) are computed at snapshot time from
// the internal HDR histogram and are not updated in place.
type FingerprintStats struct {
	FingerprintID string            // stable hash from fingerprinter
	Fingerprint   string            // normalized query text
	DigestHash    string            // MySQL STATEMENT_DIGEST (if resolved)
	DigestText    string            // MySQL STATEMENT_DIGEST_TEXT (if resolved)
	Tags          map[string]string // from comment parser (most recent wins)
	SampleQuery   string            // raw query example
	Count         uint64
	TotalNs       uint64
	MinNs         uint64
	MaxNs         uint64
	P50Ns         int64 // from histogram
	P95Ns         int64
	P99Ns         int64
	LastSeen      time.Time
	QPS           float64 // queries per second (computed from window)
}

// AggregatorStats holds operational metrics about the aggregator.
type AggregatorStats struct {
	TotalEvents        uint64
	ActiveFingerprints int
	Evictions          uint64 // from fingerprinter LRU
}
