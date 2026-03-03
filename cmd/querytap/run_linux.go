//go:build linux

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/schlubbi/query-tap/internal/aggregator"
	"github.com/schlubbi/query-tap/internal/detect"
	"github.com/schlubbi/query-tap/internal/ebpf"
	"github.com/schlubbi/query-tap/internal/pipeline"
	"github.com/schlubbi/query-tap/internal/stream"
	"github.com/spf13/cobra"
)

func runProbe(cmd *cobra.Command, agg *aggregator.Aggregator, sw *stream.Writer, logger *slog.Logger) error {
	mysqlPath, _ := cmd.Flags().GetString("mysql-path")
	pid, _ := cmd.Flags().GetInt("pid")
	streamMode, _ := cmd.Flags().GetBool("stream")

	// Detect mysqld.
	proc, err := detect.FindOrOverride(mysqlPath, pid)
	if err != nil {
		return fmt.Errorf("mysqld detection: %w", err)
	}
	logger.Info("found mysqld", "pid", proc.PID, "binary", proc.BinaryPath)

	// Load BPF and attach probes.
	probe, err := ebpf.NewProbe(proc.BinaryPath, logger)
	if err != nil {
		return fmt.Errorf("BPF probe: %w", err)
	}
	defer probe.Close()

	// Wire pipeline.
	events := make(chan ebpf.Event, 4096)
	pipe := pipeline.New(agg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Start event reader.
	go func() {
		if err := probe.ReadEvents(ctx, events); err != nil {
			logger.Error("event reader stopped", "error", err)
		}
		close(events)
	}()

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "querytap %s — tracing mysqld PID %d\n", version, proc.PID)

	if streamMode {
		// In stream mode, forward matched events to the writer.
		return runStreamPipeline(ctx, pipe, events, agg, sw)
	}

	// TUI mode — pipeline feeds aggregator, TUI polls snapshots.
	return pipe.Run(ctx, events)
}

func runStreamPipeline(ctx context.Context, pipe *pipeline.Pipeline, events <-chan ebpf.Event, agg *aggregator.Aggregator, sw *stream.Writer) error {
	// Tick for periodic snapshot output.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	go func() {
		_ = pipe.Run(ctx, events)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			snap := agg.Snapshot()
			for i := range snap {
				evt := &stream.Event{
					Timestamp:     snap[i].LastSeen,
					LatencyMs:     float64(snap[i].P50Ns) / 1e6,
					FingerprintID: snap[i].FingerprintID,
					Fingerprint:   snap[i].Fingerprint,
					DigestHash:    snap[i].DigestHash,
					QueryPreview:  truncate(snap[i].SampleQuery, 80),
					Tags:          snap[i].Tags,
				}
				if err := sw.WriteEvent(evt); err != nil {
					return fmt.Errorf("writing event: %w", err)
				}
			}
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
