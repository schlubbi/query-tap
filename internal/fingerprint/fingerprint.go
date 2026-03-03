// Package fingerprint provides SQL query normalization and an LRU fingerprint cache.
//
// It uses Percona's go-mysql library for query normalization (replacing literals
// with placeholders) and Hashicorp's LRU for bounded cardinality. SQL comments
// are stripped before fingerprinting so that comment metadata doesn't fragment
// the fingerprint space, but the original query (with comments) is retained as
// SampleQuery for later MySQL STATEMENT_DIGEST() resolution.
package fingerprint

import (
	"regexp"
	"sync/atomic"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/percona/go-mysql/query"
)

// commentRe matches C-style block comments (/* ... */) including marginalia
// and sqlcommenter annotations. These are stripped before fingerprinting so
// that comment metadata doesn't create distinct fingerprints for otherwise
// identical queries.
var commentRe = regexp.MustCompile(`/\*.*?\*/`)

// FingerprintEntry holds the normalized query and digest metadata.
type FingerprintEntry struct {
	ID             string // stable hash from percona query.Id()
	Normalized     string // normalized query from percona query.Fingerprint()
	SampleQuery    string // one raw query example (for MySQL digest resolution later)
	DigestHash     string // MySQL STATEMENT_DIGEST() — filled by digest resolver
	DigestText     string // MySQL STATEMENT_DIGEST_TEXT() — filled by digest resolver
	DigestResolved bool   // true once MySQL digest has been resolved
}

// Fingerprinter normalizes SQL queries into fingerprints with an LRU cache.
type Fingerprinter struct {
	cache     *lru.Cache[string, *FingerprintEntry]
	evictions atomic.Uint64
}

// New creates a Fingerprinter with the given maximum number of cached entries.
// When the cache is full, the least-recently-used entry is evicted.
func New(maxEntries int) *Fingerprinter {
	f := &Fingerprinter{}
	cache, err := lru.NewWithEvict[string, *FingerprintEntry](maxEntries, func(_ string, _ *FingerprintEntry) {
		f.evictions.Add(1)
	})
	if err != nil {
		// lru.New only returns an error for size <= 0, which is a programming error.
		panic("fingerprint: invalid maxEntries: " + err.Error())
	}
	f.cache = cache
	return f
}

// Fingerprint normalizes a query and returns its cached entry.
//
// SQL comments (/* ... */) are stripped before fingerprinting so that comment
// metadata doesn't fragment the fingerprint space. The original query (with
// comments) is stored as SampleQuery on first occurrence for later
// STATEMENT_DIGEST() resolution.
//
// If this fingerprint ID is already cached, the existing entry is returned
// (promoting it in the LRU). If the cache is full, the least-recently-used
// entry is evicted.
func (f *Fingerprinter) Fingerprint(rawQuery string) *FingerprintEntry {
	stripped := stripComments(rawQuery)
	normalized := query.Fingerprint(stripped)
	id := query.Id(normalized)

	if entry, ok := f.cache.Get(id); ok {
		return entry
	}

	entry := &FingerprintEntry{
		ID:          id,
		Normalized:  normalized,
		SampleQuery: rawQuery,
	}
	f.cache.Add(id, entry)
	return entry
}

// Unresolved returns all cached entries that have not yet had their MySQL
// digest resolved via SetDigest.
func (f *Fingerprinter) Unresolved() []*FingerprintEntry {
	var out []*FingerprintEntry
	for _, entry := range f.cache.Values() {
		if !entry.DigestResolved {
			out = append(out, entry)
		}
	}
	return out
}

// SetDigest updates an entry with MySQL-native digest data obtained from
// STATEMENT_DIGEST() / STATEMENT_DIGEST_TEXT(). If the ID is not in the
// cache (e.g., it was evicted), this is a no-op.
func (f *Fingerprinter) SetDigest(id, digestHash, digestText string) {
	entry, ok := f.cache.Peek(id)
	if !ok {
		return
	}
	entry.DigestHash = digestHash
	entry.DigestText = digestText
	entry.DigestResolved = true
}

// Evictions returns the total number of LRU evictions since creation.
func (f *Fingerprinter) Evictions() uint64 {
	return f.evictions.Load()
}

// Len returns the number of entries currently in the cache.
func (f *Fingerprinter) Len() int {
	return f.cache.Len()
}

// stripComments removes C-style block comments (/* ... */) from a query.
func stripComments(q string) string {
	return commentRe.ReplaceAllString(q, "")
}
