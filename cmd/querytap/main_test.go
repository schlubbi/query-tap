package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCmdPrintsVersion(t *testing.T) {
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "querytap dev") {
		t.Errorf("expected output to contain 'querytap dev', got %q", got)
	}
}

func TestRootCmdVersionFlag(t *testing.T) {
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "dev") {
		t.Errorf("expected version output to contain 'dev', got %q", got)
	}
}

func TestRootCmdHasAllFlags(t *testing.T) {
	cmd := newRootCmd()

	expectedFlags := []string{
		"mysql-path",
		"pid",
		"stream",
		"format",
		"max-fingerprints",
		"max-query-len",
		"ringbuf-size",
		"export",
		"otlp-endpoint",
		"dogstatsd-addr",
		"filter",
		"comment-parser",
		"top",
		"interval",
		"mysql-dsn",
		"verbose",
	}

	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag --%s to be registered", name)
		}
	}
}
