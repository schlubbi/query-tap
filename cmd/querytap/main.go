// Package main is the entry point for the querytap CLI.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"

	"github.com/schlubbi/query-tap/internal/aggregator"
	"github.com/schlubbi/query-tap/internal/comment"
	"github.com/schlubbi/query-tap/internal/fingerprint"
	"github.com/schlubbi/query-tap/internal/stream"
	"github.com/spf13/cobra"
)

// version is set at build time via ldflags.
var version = "dev"

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "querytap",
		Short: "Zero-instrumentation MySQL query observer using eBPF",
		Long: `QueryTap attaches eBPF uprobes to a running mysqld process and captures
every SQL statement in real time. Queries are normalized into fingerprints
and aggregated with per-fingerprint latency metrics (count, p50, p99, max).

Output is available as a live TUI dashboard or a streaming JSON/text feed.
Requires Linux kernel ≥5.8 and root or CAP_BPF+CAP_PERFMON.`,
		Version:      version,
		SilenceUsage: true,
	}

	f := cmd.Flags()

	// Target selection
	f.String("mysql-path", "", "Path to mysqld binary (auto-detected if omitted)")
	f.Int("pid", 0, "Attach to a specific mysqld process ID")

	// Output mode
	f.Bool("stream", false, "Enable streaming output mode (disables TUI)")
	f.String("format", "text", "Output format for stream mode: text, json, csv")

	// Fingerprinting
	f.Int("max-fingerprints", 10000, "Maximum number of unique fingerprints to track")
	f.Int("max-query-len", 4096, "Maximum query length captured from BPF ring buffer (bytes)")

	// BPF tuning
	f.Int("ringbuf-size", 16, "BPF ring buffer size in MiB (must be power of 2)")

	// Telemetry export
	f.String("export", "", "Export format: otlp, dogstatsd")
	f.String("otlp-endpoint", "", "OTLP gRPC endpoint for metric export")
	f.String("dogstatsd-addr", "", "DogStatsD address for metric export (host:port)")

	// Filtering
	f.String("filter", "", "Regex filter applied to raw SQL text")

	// Comment parsing
	f.String("comment-parser", "", "Comment parser: marginalia, sqlcommenter, or custom")

	// TUI options
	f.Int("top", 20, "Number of fingerprints to display in TUI mode")
	f.Duration("interval", 0, "Refresh interval for TUI mode (default 1s)")

	// MySQL connection (for STATEMENT_DIGEST)
	f.String("mysql-dsn", "", "MySQL DSN for STATEMENT_DIGEST resolution")

	// Debugging
	f.BoolP("verbose", "v", false, "Enable verbose/debug logging")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runRoot(cmd)
	}

	return cmd
}

func runRoot(cmd *cobra.Command) error {
	streamMode, _ := cmd.Flags().GetBool("stream")
	format, _ := cmd.Flags().GetString("format")
	maxFingerprints, _ := cmd.Flags().GetInt("max-fingerprints")
	commentParserName, _ := cmd.Flags().GetString("comment-parser")
	filterPattern, _ := cmd.Flags().GetString("filter")
	verbose, _ := cmd.Flags().GetBool("verbose")

	// Validate and compile filter regex if provided.
	if filterPattern != "" {
		if _, err := regexp.Compile(filterPattern); err != nil {
			return fmt.Errorf("invalid --filter regex: %w", err)
		}
	}

	// Look up comment parser if specified.
	var parser comment.CommentParser
	if commentParserName != "" {
		var err error
		parser, err = comment.Get(commentParserName)
		if err != nil {
			return err
		}
	}

	// Build logger.
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{
		Level: logLevel,
	}))

	logger.Debug("configuration",
		"stream", streamMode,
		"format", format,
		"max_fingerprints", maxFingerprints,
		"comment_parser", commentParserName,
		"filter", filterPattern,
	)

	// Build pipeline components.
	fp := fingerprint.New(maxFingerprints)
	agg := aggregator.New(fp, parser)

	var sw *stream.Writer
	if streamMode {
		sw = stream.New(cmd.OutOrStdout(), format)
	}

	return runProbe(cmd, agg, sw, logger)
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
