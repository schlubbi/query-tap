package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCmdDefaultPrintsRequiresLinux(t *testing.T) {
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "requires Linux") {
		t.Errorf("expected Linux requirement message, got %q", got)
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

func TestRootCmdStreamModeNoError(t *testing.T) {
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--stream", "--format=json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--stream --format=json returned error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "requires Linux") {
		t.Errorf("expected Linux requirement message, got %q", got)
	}
}

func TestRootCmdStreamModeTextFormat(t *testing.T) {
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--stream", "--format=text"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--stream --format=text returned error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "requires Linux") {
		t.Errorf("expected Linux requirement message, got %q", got)
	}
}

func TestRootCmdCommentParserRails(t *testing.T) {
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--stream", "--comment-parser=rails"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--comment-parser=rails returned error: %v", err)
	}
}

func TestRootCmdCommentParserMarginalia(t *testing.T) {
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--stream", "--comment-parser=marginalia"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--comment-parser=marginalia returned error: %v", err)
	}
}

func TestRootCmdCommentParserUnknownReturnsError(t *testing.T) {
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--stream", "--comment-parser=unknown"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown comment parser, got nil")
	}
	if !strings.Contains(err.Error(), "unknown comment parser") {
		t.Errorf("expected 'unknown comment parser' in error, got %q", err.Error())
	}
}

func TestRootCmdUnknownFlagReturnsError(t *testing.T) {
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--nonexistent-flag"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
}

func TestRootCmdFilterFlagAccepted(t *testing.T) {
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--stream", "--filter=SELECT.*users"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--filter flag returned error: %v", err)
	}
}

func TestRootCmdFilterInvalidRegexReturnsError(t *testing.T) {
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--stream", "--filter=[invalid"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid regex filter, got nil")
	}
	if !strings.Contains(err.Error(), "filter") {
		t.Errorf("expected 'filter' in error message, got %q", err.Error())
	}
}
