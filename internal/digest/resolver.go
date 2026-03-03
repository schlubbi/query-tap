// Package digest provides MySQL STATEMENT_DIGEST resolution via a live MySQL connection.
//
// The Resolver runs a background goroutine that periodically queries MySQL to
// resolve query fingerprints into canonical MySQL digests using
// STATEMENT_DIGEST() and STATEMENT_DIGEST_TEXT() (available in MySQL 8.0.4+).
package digest

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/schlubbi/query-tap/internal/fingerprint"
)

const (
	defaultBatchSize = 100
	defaultInterval  = 10 * time.Second
)

// Resolver periodically resolves query fingerprints to MySQL-native digests.
type Resolver struct {
	db            *sql.DB
	fingerprinter *fingerprint.Fingerprinter
	interval      time.Duration
	batchSize     int
	logger        *slog.Logger
}

// New creates a digest resolver that connects to MySQL via the given DSN.
// The DSN format follows go-sql-driver/mysql conventions
// (e.g., "user:pass@tcp(host:port)/").
// If interval is zero or negative, defaults to 10 seconds.
// Returns an error if the MySQL connection cannot be established.
func New(dsn string, fp *fingerprint.Fingerprinter, interval time.Duration, logger *slog.Logger) (*Resolver, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("digest: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("digest: ping: %w", err)
	}

	// Verify STATEMENT_DIGEST() availability (requires MySQL 8.0.4+).
	var testHash string
	if err := db.QueryRow("SELECT STATEMENT_DIGEST('SELECT 1')").Scan(&testHash); err != nil {
		logger.Warn("STATEMENT_DIGEST() unavailable, requires MySQL 8.0.4+", "error", err)
	}

	return newResolver(db, fp, interval, logger), nil
}

// newResolver creates a Resolver from an already-opened *sql.DB.
// Used internally and by tests with sqlmock.
func newResolver(db *sql.DB, fp *fingerprint.Fingerprinter, interval time.Duration, logger *slog.Logger) *Resolver {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Resolver{
		db:            db,
		fingerprinter: fp,
		interval:      interval,
		batchSize:     defaultBatchSize,
		logger:        logger,
	}
}

// Run starts the background resolution loop. It runs one resolution cycle
// immediately, then repeats on the configured interval until ctx is cancelled.
func (r *Resolver) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	r.logCycle(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.logCycle(ctx)
		}
	}
}

// logCycle runs one resolution cycle and logs non-trivial results.
func (r *Resolver) logCycle(ctx context.Context) {
	resolved, errors := r.resolveOnce(ctx)
	if resolved > 0 || errors > 0 {
		r.logger.Info("digest resolution cycle", "resolved", resolved, "errors", errors)
	}
}

// resolveOnce processes one batch of unresolved fingerprints.
// Returns counts of successfully resolved entries and errors encountered.
func (r *Resolver) resolveOnce(ctx context.Context) (resolved int, errors int) {
	unresolved := r.fingerprinter.Unresolved()
	if len(unresolved) == 0 {
		return 0, 0
	}

	// Respect batch size to bound work per cycle.
	if len(unresolved) > r.batchSize {
		unresolved = unresolved[:r.batchSize]
	}

	for _, entry := range unresolved {
		if ctx.Err() != nil {
			return resolved, errors
		}

		var digestHash, digestText string
		err := r.db.QueryRowContext(ctx,
			"SELECT STATEMENT_DIGEST(?) AS digest_hash, STATEMENT_DIGEST_TEXT(?) AS digest_text",
			entry.SampleQuery, entry.SampleQuery,
		).Scan(&digestHash, &digestText)
		if err != nil {
			// Don't count context cancellation as a resolution error.
			if ctx.Err() != nil {
				return resolved, errors
			}
			r.logger.Warn("digest resolution failed",
				"fingerprint_id", entry.ID,
				"error", err,
			)
			errors++
			continue
		}

		r.fingerprinter.SetDigest(entry.ID, digestHash, digestText)
		resolved++
	}

	return resolved, errors
}

// Close closes the underlying database connection.
func (r *Resolver) Close() error {
	return r.db.Close()
}
