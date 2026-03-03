//go:build !linux

package main

import (
	"fmt"
	"log/slog"

	"github.com/schlubbi/query-tap/internal/aggregator"
	"github.com/schlubbi/query-tap/internal/stream"
	"github.com/spf13/cobra"
)

func runProbe(cmd *cobra.Command, _ *aggregator.Aggregator, _ *stream.Writer, _ *slog.Logger) error {
	_, _ = fmt.Fprintln(cmd.OutOrStdout(),
		"QueryTap requires Linux with kernel ≥5.8 for eBPF support.")
	return nil
}
