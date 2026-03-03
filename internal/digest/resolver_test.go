package digest

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/schlubbi/query-tap/internal/fingerprint"
)

func newTestResolver(t *testing.T) (*Resolver, sqlmock.Sqlmock, *fingerprint.Fingerprinter) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	fp := fingerprint.New(100)
	r := newResolver(db, fp, time.Second, slog.Default())
	return r, mock, fp
}

func TestResolveOnceResolvesAllUnresolved(t *testing.T) {
	r, mock, fp := newTestResolver(t)

	fp.Fingerprint("SELECT * FROM users WHERE id = 1")
	fp.Fingerprint("SELECT * FROM orders WHERE user_id = 2")
	fp.Fingerprint("SELECT * FROM products WHERE name = 'test'")

	entries := fp.Unresolved()
	if len(entries) != 3 {
		t.Fatalf("precondition: expected 3 unresolved entries, got %d", len(entries))
	}

	for _, entry := range entries {
		mock.ExpectQuery("SELECT STATEMENT_DIGEST").
			WithArgs(entry.SampleQuery, entry.SampleQuery).
			WillReturnRows(sqlmock.NewRows([]string{"digest_hash", "digest_text"}).
				AddRow("hash_"+entry.ID, "text_"+entry.ID))
	}

	resolved, errors := r.resolveOnce(context.Background())

	if resolved != 3 {
		t.Errorf("expected 3 resolved, got %d", resolved)
	}
	if errors != 0 {
		t.Errorf("expected 0 errors, got %d", errors)
	}

	// Verify all entries are now resolved with correct values.
	if remaining := len(fp.Unresolved()); remaining != 0 {
		t.Errorf("expected 0 unresolved after resolve, got %d", remaining)
	}
	for _, entry := range entries {
		if !entry.DigestResolved {
			t.Errorf("entry %s should be resolved", entry.ID)
		}
		if want := "hash_" + entry.ID; entry.DigestHash != want {
			t.Errorf("entry %s: expected DigestHash %q, got %q", entry.ID, want, entry.DigestHash)
		}
		if want := "text_" + entry.ID; entry.DigestText != want {
			t.Errorf("entry %s: expected DigestText %q, got %q", entry.ID, want, entry.DigestText)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestResolveOnceSkipsFailedQueries(t *testing.T) {
	r, mock, fp := newTestResolver(t)

	fp.Fingerprint("SELECT * FROM users WHERE id = 1")
	fp.Fingerprint("SELECT * FROM orders WHERE user_id = 2")
	fp.Fingerprint("SELECT * FROM products WHERE name = 'test'")

	entries := fp.Unresolved()

	// First entry succeeds.
	mock.ExpectQuery("SELECT STATEMENT_DIGEST").
		WithArgs(entries[0].SampleQuery, entries[0].SampleQuery).
		WillReturnRows(sqlmock.NewRows([]string{"digest_hash", "digest_text"}).
			AddRow("hash1", "text1"))

	// Second entry fails.
	mock.ExpectQuery("SELECT STATEMENT_DIGEST").
		WithArgs(entries[1].SampleQuery, entries[1].SampleQuery).
		WillReturnError(fmt.Errorf("parse error"))

	// Third entry succeeds.
	mock.ExpectQuery("SELECT STATEMENT_DIGEST").
		WithArgs(entries[2].SampleQuery, entries[2].SampleQuery).
		WillReturnRows(sqlmock.NewRows([]string{"digest_hash", "digest_text"}).
			AddRow("hash3", "text3"))

	resolved, errors := r.resolveOnce(context.Background())

	if resolved != 2 {
		t.Errorf("expected 2 resolved, got %d", resolved)
	}
	if errors != 1 {
		t.Errorf("expected 1 error, got %d", errors)
	}
	if remaining := len(fp.Unresolved()); remaining != 1 {
		t.Errorf("expected 1 unresolved after partial resolve, got %d", remaining)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestResolveOnceWithNoUnresolved(t *testing.T) {
	r, mock, _ := newTestResolver(t)

	resolved, errors := r.resolveOnce(context.Background())

	if resolved != 0 {
		t.Errorf("expected 0 resolved, got %d", resolved)
	}
	if errors != 0 {
		t.Errorf("expected 0 errors, got %d", errors)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestResolveOnceBatchSizeRespected(t *testing.T) {
	r, mock, fp := newTestResolver(t)
	r.batchSize = 2

	fp.Fingerprint("SELECT * FROM a")
	fp.Fingerprint("SELECT * FROM b")
	fp.Fingerprint("SELECT * FROM c")

	entries := fp.Unresolved()
	if len(entries) != 3 {
		t.Fatalf("precondition: expected 3 unresolved, got %d", len(entries))
	}

	// Only the first 2 should be processed due to batchSize=2.
	for _, entry := range entries[:2] {
		mock.ExpectQuery("SELECT STATEMENT_DIGEST").
			WithArgs(entry.SampleQuery, entry.SampleQuery).
			WillReturnRows(sqlmock.NewRows([]string{"digest_hash", "digest_text"}).
				AddRow("hash", "text"))
	}

	resolved, errors := r.resolveOnce(context.Background())

	if resolved != 2 {
		t.Errorf("expected 2 resolved (batch limit), got %d", resolved)
	}
	if errors != 0 {
		t.Errorf("expected 0 errors, got %d", errors)
	}
	if remaining := len(fp.Unresolved()); remaining != 1 {
		t.Errorf("expected 1 unresolved after batch-limited resolve, got %d", remaining)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestRunStopsOnContextCancellation(t *testing.T) {
	r, _, _ := newTestResolver(t)
	r.interval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- r.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within timeout after context cancellation")
	}
}

func TestNewWithUnreachableDSNReturnsError(t *testing.T) {
	fp := fingerprint.New(100)
	r, err := New("root:pass@tcp(127.0.0.1:1)/?timeout=100ms", fp, time.Second, slog.Default())
	if err == nil {
		r.Close()
		t.Fatal("expected error for unreachable MySQL DSN, got nil")
	}
}
