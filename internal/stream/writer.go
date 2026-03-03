// Package stream provides streaming stdout output in JSON and text formats.
package stream

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Event represents a single query event ready for output.
type Event struct {
	Timestamp     time.Time
	LatencyMs     float64
	FingerprintID string
	Fingerprint   string
	DigestHash    string            // MySQL digest (may be empty if not yet resolved)
	QueryPreview  string            // first 80 chars of raw query
	Tags          map[string]string // from comment parser
}

// jsonEvent is the JSON-serializable form of Event.
type jsonEvent struct {
	Timestamp     string            `json:"timestamp"`
	LatencyMs     float64           `json:"latency_ms"`
	FingerprintID string            `json:"fingerprint_id"`
	Fingerprint   string            `json:"fingerprint"`
	DigestHash    string            `json:"digest_hash,omitempty"`
	QueryPreview  string            `json:"query_preview"`
	Tags          map[string]string `json:"tags"`
}

// Writer formats query events for stdout output.
type Writer struct {
	out    io.Writer
	format string // "text" or "json"
}

// New creates a Writer that writes events to out in the given format.
// Supported formats: "text", "json".
func New(out io.Writer, format string) *Writer {
	return &Writer{out: out, format: format}
}

// WriteEvent writes a single query event to the output.
func (w *Writer) WriteEvent(evt *Event) error {
	switch w.format {
	case "json":
		return w.writeJSON(evt)
	default:
		return w.writeText(evt)
	}
}

// writeText formats: TIMESTAMP  LATENCYms  FINGERPRINT_ID  FINGERPRINT  [tags]
func (w *Writer) writeText(evt *Event) error {
	var sb strings.Builder
	sb.WriteString(evt.Timestamp.UTC().Format(time.RFC3339))
	sb.WriteString("  ")
	sb.WriteString(fmt.Sprintf("%.2fms", evt.LatencyMs))
	sb.WriteString("  ")
	sb.WriteString(evt.FingerprintID)
	sb.WriteString("  ")
	sb.WriteString(evt.Fingerprint)

	if len(evt.Tags) > 0 {
		sb.WriteString("  [")
		sb.WriteString(formatTags(evt.Tags))
		sb.WriteString("]")
	}

	sb.WriteString("\n")
	_, err := fmt.Fprint(w.out, sb.String())
	return err
}

// writeJSON writes a single NDJSON line.
func (w *Writer) writeJSON(evt *Event) error {
	tags := evt.Tags
	if tags == nil {
		tags = make(map[string]string)
	}

	je := jsonEvent{
		Timestamp:     evt.Timestamp.UTC().Format(time.RFC3339),
		LatencyMs:     evt.LatencyMs,
		FingerprintID: evt.FingerprintID,
		Fingerprint:   evt.Fingerprint,
		DigestHash:    evt.DigestHash,
		QueryPreview:  evt.QueryPreview,
		Tags:          tags,
	}

	data, err := json.Marshal(je)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w.out, "%s\n", data)
	return err
}

// formatTags renders tags as sorted key=value pairs separated by commas.
func formatTags(tags map[string]string) string {
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+tags[k])
	}
	return strings.Join(parts, ",")
}
