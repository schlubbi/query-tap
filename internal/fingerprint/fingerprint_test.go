package fingerprint

import (
	"sync"
	"testing"
)

func TestFingerprintNormalizesLiterals(t *testing.T) {
	fp := New(100)

	entry := fp.Fingerprint("SELECT * FROM users WHERE id = 123")
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Normalized == "" {
		t.Fatal("expected non-empty normalized query")
	}
	if entry.Normalized == "SELECT * FROM users WHERE id = 123" {
		t.Errorf("expected literals to be normalized, got %q", entry.Normalized)
	}
	// Percona replaces literals with '?'
	if got := entry.Normalized; got != "select * from users where id = ?" {
		t.Errorf("expected 'select * from users where id = ?', got %q", got)
	}
}

func TestSameFingerprintForDifferentLiterals(t *testing.T) {
	fp := New(100)

	e1 := fp.Fingerprint("SELECT * FROM users WHERE id = 123")
	e2 := fp.Fingerprint("SELECT * FROM users WHERE id = 456")

	if e1.ID != e2.ID {
		t.Errorf("expected same fingerprint ID, got %q and %q", e1.ID, e2.ID)
	}
}

func TestValueListsCollapsed(t *testing.T) {
	fp := New(100)

	entry := fp.Fingerprint("INSERT INTO t VALUES (1,2,3),(4,5,6)")
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	// Percona collapses value lists
	if got := entry.Normalized; got != "insert into t values(?+)" {
		t.Errorf("expected collapsed value list, got %q", got)
	}
}

func TestCommentStrippedBeforeFingerprinting(t *testing.T) {
	fp := New(100)

	withComment := fp.Fingerprint("/* app=web */ SELECT * FROM users WHERE id = 1")
	withoutComment := fp.Fingerprint("SELECT * FROM users WHERE id = 2")

	if withComment.ID != withoutComment.ID {
		t.Errorf("expected same fingerprint ID with and without comment, got %q and %q",
			withComment.ID, withoutComment.ID)
	}
}

func TestSampleQueryRetainsOriginalComment(t *testing.T) {
	fp := New(100)

	original := "/* app=web */ SELECT * FROM users WHERE id = 1"
	entry := fp.Fingerprint(original)

	if entry.SampleQuery != original {
		t.Errorf("expected SampleQuery to retain original query %q, got %q",
			original, entry.SampleQuery)
	}
}

func TestSampleQueryNotOverwrittenOnSecondCall(t *testing.T) {
	fp := New(100)

	first := "SELECT * FROM users WHERE id = 1"
	second := "SELECT * FROM users WHERE id = 2"

	fp.Fingerprint(first)
	entry := fp.Fingerprint(second)

	if entry.SampleQuery != first {
		t.Errorf("expected SampleQuery to remain %q (first seen), got %q",
			first, entry.SampleQuery)
	}
}

func TestLRUEviction(t *testing.T) {
	fp := New(3)

	// Fill the cache with 3 distinct fingerprints
	fp.Fingerprint("SELECT * FROM a")
	fp.Fingerprint("SELECT * FROM b")
	fp.Fingerprint("SELECT * FROM c")

	if fp.Len() != 3 {
		t.Fatalf("expected cache length 3, got %d", fp.Len())
	}
	if fp.Evictions() != 0 {
		t.Fatalf("expected 0 evictions, got %d", fp.Evictions())
	}

	// Adding a 4th distinct fingerprint should evict the oldest
	fp.Fingerprint("SELECT * FROM d")

	if fp.Len() != 3 {
		t.Errorf("expected cache length 3 after eviction, got %d", fp.Len())
	}
	if fp.Evictions() != 1 {
		t.Errorf("expected 1 eviction, got %d", fp.Evictions())
	}
}

func TestEmptyQuery(t *testing.T) {
	fp := New(100)

	entry := fp.Fingerprint("")
	if entry == nil {
		t.Fatal("expected non-nil entry for empty query")
	}
	// Should not panic and should return a valid entry
	if entry.ID == "" {
		t.Error("expected non-empty ID even for empty query")
	}
}

func TestUnresolvedReturnsAllInitially(t *testing.T) {
	fp := New(100)

	fp.Fingerprint("SELECT * FROM a")
	fp.Fingerprint("SELECT * FROM b")
	fp.Fingerprint("SELECT * FROM c")

	unresolved := fp.Unresolved()
	if len(unresolved) != 3 {
		t.Errorf("expected 3 unresolved entries, got %d", len(unresolved))
	}

	for _, e := range unresolved {
		if e.DigestResolved {
			t.Errorf("expected entry %q to not be resolved", e.ID)
		}
	}
}

func TestSetDigestMarksResolved(t *testing.T) {
	fp := New(100)

	entry := fp.Fingerprint("SELECT * FROM users WHERE id = 1")
	fp.SetDigest(entry.ID, "abc123hash", "SELECT * FROM users WHERE id = ?")

	// Fetch the entry again to verify it's updated
	updated := fp.Fingerprint("SELECT * FROM users WHERE id = 2")

	if !updated.DigestResolved {
		t.Error("expected entry to be marked as resolved")
	}
	if updated.DigestHash != "abc123hash" {
		t.Errorf("expected DigestHash 'abc123hash', got %q", updated.DigestHash)
	}
	if updated.DigestText != "SELECT * FROM users WHERE id = ?" {
		t.Errorf("expected DigestText 'SELECT * FROM users WHERE id = ?', got %q", updated.DigestText)
	}
}

func TestSetDigestRemovedFromUnresolved(t *testing.T) {
	fp := New(100)

	fp.Fingerprint("SELECT * FROM a")
	e2 := fp.Fingerprint("SELECT * FROM b")
	fp.Fingerprint("SELECT * FROM c")

	fp.SetDigest(e2.ID, "hash", "text")

	unresolved := fp.Unresolved()
	if len(unresolved) != 2 {
		t.Errorf("expected 2 unresolved entries after SetDigest, got %d", len(unresolved))
	}

	for _, e := range unresolved {
		if e.ID == e2.ID {
			t.Errorf("entry %q should not be in unresolved after SetDigest", e2.ID)
		}
	}
}

func TestSetDigestNonexistentIDIsNoop(t *testing.T) {
	fp := New(100)

	// Should not panic
	fp.SetDigest("nonexistent", "hash", "text")
}

func TestConcurrentAccess(t *testing.T) {
	fp := New(1000)

	var wg sync.WaitGroup
	queries := []string{
		"SELECT * FROM users WHERE id = 1",
		"SELECT * FROM orders WHERE user_id = 2",
		"INSERT INTO logs VALUES (1, 'test')",
		"UPDATE users SET name = 'alice' WHERE id = 3",
		"DELETE FROM sessions WHERE expired = 1",
	}

	// Spawn multiple goroutines fingerprinting concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			q := queries[idx%len(queries)]
			entry := fp.Fingerprint(q)
			if entry == nil {
				t.Errorf("expected non-nil entry for query %q", q)
			}
		}(i)
	}
	wg.Wait()

	if fp.Len() != len(queries) {
		t.Errorf("expected %d entries, got %d", len(queries), fp.Len())
	}
}

func TestConcurrentFingerprintAndSetDigest(t *testing.T) {
	fp := New(1000)

	var wg sync.WaitGroup

	// Fingerprint in parallel
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fp.Fingerprint("SELECT * FROM users WHERE id = 1")
		}(i)
	}

	// SetDigest in parallel
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// ID might not exist yet, that's fine — tests for no-panic
			fp.SetDigest("some-id", "hash", "text")
		}()
	}

	wg.Wait()
}
