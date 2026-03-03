package stream

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var testTime = time.Date(2026, 3, 3, 21, 0, 0, 0, time.UTC)

func TestWriteEventTextFormat(t *testing.T) {
	buf := new(bytes.Buffer)
	w := New(buf, "text")

	evt := &Event{
		Timestamp:     testTime,
		LatencyMs:     12.34,
		FingerprintID: "abc123def",
		Fingerprint:   "SELECT * FROM users WHERE id = ?",
		DigestHash:    "deadbeef",
		QueryPreview:  "SELECT * FROM users WHERE id = 42",
		Tags:          map[string]string{"app": "web", "endpoint": "/api/users"},
	}

	if err := w.WriteEvent(evt); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "2026-03-03T21:00:00Z") {
		t.Errorf("expected timestamp in output, got %q", got)
	}
	if !strings.Contains(got, "12.34ms") {
		t.Errorf("expected latency in output, got %q", got)
	}
	if !strings.Contains(got, "abc123def") {
		t.Errorf("expected fingerprint ID in output, got %q", got)
	}
	if !strings.Contains(got, "SELECT * FROM users WHERE id = ?") {
		t.Errorf("expected fingerprint in output, got %q", got)
	}
	if !strings.Contains(got, "app=web") {
		t.Errorf("expected tag app=web in output, got %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("expected output to end with newline, got %q", got)
	}
}

func TestWriteEventTextFormatEmptyTags(t *testing.T) {
	buf := new(bytes.Buffer)
	w := New(buf, "text")

	evt := &Event{
		Timestamp:     testTime,
		LatencyMs:     0.50,
		FingerprintID: "xyz789",
		Fingerprint:   "INSERT INTO logs VALUES (?)",
		QueryPreview:  "INSERT INTO logs VALUES ('hello')",
	}

	if err := w.WriteEvent(evt); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}

	got := buf.String()
	if strings.Contains(got, "[") {
		t.Errorf("expected no tags section for empty tags, got %q", got)
	}
}

func TestWriteEventJSONFormat(t *testing.T) {
	buf := new(bytes.Buffer)
	w := New(buf, "json")

	evt := &Event{
		Timestamp:     testTime,
		LatencyMs:     12.34,
		FingerprintID: "abc123def",
		Fingerprint:   "SELECT * FROM users WHERE id = ?",
		DigestHash:    "deadbeef",
		QueryPreview:  "SELECT * FROM users WHERE id = 42",
		Tags:          map[string]string{"app": "web"},
	}

	if err := w.WriteEvent(evt); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}

	got := buf.String()

	// Must be valid JSON.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %q", err, got)
	}

	// Check required fields.
	if ts, ok := parsed["timestamp"].(string); !ok || ts != "2026-03-03T21:00:00Z" {
		t.Errorf("expected timestamp 2026-03-03T21:00:00Z, got %v", parsed["timestamp"])
	}
	if lat, ok := parsed["latency_ms"].(float64); !ok || lat != 12.34 {
		t.Errorf("expected latency_ms 12.34, got %v", parsed["latency_ms"])
	}
	if fpID, ok := parsed["fingerprint_id"].(string); !ok || fpID != "abc123def" {
		t.Errorf("expected fingerprint_id abc123def, got %v", parsed["fingerprint_id"])
	}
	if dh, ok := parsed["digest_hash"].(string); !ok || dh != "deadbeef" {
		t.Errorf("expected digest_hash deadbeef, got %v", parsed["digest_hash"])
	}
	if tags, ok := parsed["tags"].(map[string]interface{}); !ok || tags["app"] != "web" {
		t.Errorf("expected tags.app=web, got %v", parsed["tags"])
	}

	// Must end with newline (NDJSON).
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("expected NDJSON line to end with newline, got %q", got)
	}
}

func TestWriteEventJSONFormatEmptyDigest(t *testing.T) {
	buf := new(bytes.Buffer)
	w := New(buf, "json")

	evt := &Event{
		Timestamp:     testTime,
		LatencyMs:     1.00,
		FingerprintID: "xyz789",
		Fingerprint:   "INSERT INTO logs VALUES (?)",
		QueryPreview:  "INSERT INTO logs VALUES ('hello')",
	}

	if err := w.WriteEvent(evt); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}

	got := buf.String()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %q", err, got)
	}

	// Empty digest hash should be omitted (omitempty).
	if _, ok := parsed["digest_hash"]; ok {
		t.Errorf("expected digest_hash to be omitted when empty, got %v", parsed["digest_hash"])
	}

	// Empty tags should be empty object, not omitted.
	if tags, ok := parsed["tags"].(map[string]interface{}); !ok || len(tags) != 0 {
		t.Errorf("expected empty tags object, got %v", parsed["tags"])
	}
}

func TestWriteEventJSONFormatEmptyTags(t *testing.T) {
	buf := new(bytes.Buffer)
	w := New(buf, "json")

	evt := &Event{
		Timestamp:     testTime,
		LatencyMs:     2.00,
		FingerprintID: "aaa111",
		Fingerprint:   "SELECT 1",
		QueryPreview:  "SELECT 1",
		Tags:          nil,
	}

	if err := w.WriteEvent(evt); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(buf.String()), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// nil tags → empty object in JSON.
	if tags, ok := parsed["tags"].(map[string]interface{}); !ok || len(tags) != 0 {
		t.Errorf("expected empty tags object for nil tags, got %v", parsed["tags"])
	}
}
