package ebpf

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// buildQueryEventBytes constructs a raw ring-buffer record for a QueryEvent.
func buildQueryEventBytes(ts uint64, tid uint32, cmd uint8, query string) []byte {
	buf := new(bytes.Buffer)
	buf.WriteByte(EventTypeQuery) // 1-byte type prefix

	_ = binary.Write(buf, binary.LittleEndian, ts)
	_ = binary.Write(buf, binary.LittleEndian, tid)
	buf.WriteByte(cmd)

	qLen := uint16(len(query))
	_ = binary.Write(buf, binary.LittleEndian, qLen)

	var qBuf [4096]byte
	copy(qBuf[:], query)
	buf.Write(qBuf[:])

	return buf.Bytes()
}

// buildLatencyEventBytes constructs a raw ring-buffer record for a LatencyEvent.
func buildLatencyEventBytes(ts uint64, tid uint32, latNs uint64) []byte {
	buf := new(bytes.Buffer)
	buf.WriteByte(EventTypeLatency) // 1-byte type prefix

	_ = binary.Write(buf, binary.LittleEndian, ts)
	_ = binary.Write(buf, binary.LittleEndian, tid)
	_ = binary.Write(buf, binary.LittleEndian, latNs)

	return buf.Bytes()
}

func TestDecodeQueryEvent(t *testing.T) {
	data := buildQueryEventBytes(123456789, 42, 3, "SELECT 1")

	ev, err := DecodeEvent(data)
	if err != nil {
		t.Fatalf("DecodeEvent returned unexpected error: %v", err)
	}

	qe, ok := ev.(*QueryEvent)
	if !ok {
		t.Fatalf("expected *QueryEvent, got %T", ev)
	}

	if qe.TimestampNs != 123456789 {
		t.Errorf("TimestampNs = %d, want 123456789", qe.TimestampNs)
	}
	if qe.TID != 42 {
		t.Errorf("TID = %d, want 42", qe.TID)
	}
	if qe.Command != 3 {
		t.Errorf("Command = %d, want 3", qe.Command)
	}
	if qe.QueryLen != 8 {
		t.Errorf("QueryLen = %d, want 8", qe.QueryLen)
	}
	if qe.QueryString() != "SELECT 1" {
		t.Errorf("QueryString() = %q, want %q", qe.QueryString(), "SELECT 1")
	}
	if qe.EventType() != EventTypeQuery {
		t.Errorf("EventType() = %d, want %d", qe.EventType(), EventTypeQuery)
	}
}

func TestDecodeLatencyEvent(t *testing.T) {
	data := buildLatencyEventBytes(999000111, 77, 5000000)

	ev, err := DecodeEvent(data)
	if err != nil {
		t.Fatalf("DecodeEvent returned unexpected error: %v", err)
	}

	le, ok := ev.(*LatencyEvent)
	if !ok {
		t.Fatalf("expected *LatencyEvent, got %T", ev)
	}

	if le.TimestampNs != 999000111 {
		t.Errorf("TimestampNs = %d, want 999000111", le.TimestampNs)
	}
	if le.TID != 77 {
		t.Errorf("TID = %d, want 77", le.TID)
	}
	if le.LatencyNs != 5000000 {
		t.Errorf("LatencyNs = %d, want 5000000", le.LatencyNs)
	}
	if le.EventType() != EventTypeLatency {
		t.Errorf("EventType() = %d, want %d", le.EventType(), EventTypeLatency)
	}
}

func TestDecodeEvent_TruncatedQueryEvent(t *testing.T) {
	// Only 5 bytes after the type prefix — far too short for a QueryEvent.
	data := []byte{EventTypeQuery, 0x01, 0x02, 0x03, 0x04, 0x05}

	_, err := DecodeEvent(data)
	if err == nil {
		t.Fatal("expected error for truncated QueryEvent, got nil")
	}
}

func TestDecodeEvent_TruncatedLatencyEvent(t *testing.T) {
	// Only 5 bytes after the type prefix — far too short for a LatencyEvent.
	data := []byte{EventTypeLatency, 0x01, 0x02, 0x03, 0x04, 0x05}

	_, err := DecodeEvent(data)
	if err == nil {
		t.Fatal("expected error for truncated LatencyEvent, got nil")
	}
}

func TestDecodeEvent_UnknownType(t *testing.T) {
	data := []byte{99, 0x00, 0x00}

	_, err := DecodeEvent(data)
	if err == nil {
		t.Fatal("expected error for unknown event type, got nil")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("error message %q should contain the unknown type value 99", err.Error())
	}
}

func TestDecodeEvent_EmptyData(t *testing.T) {
	_, err := DecodeEvent(nil)
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}

	_, err = DecodeEvent([]byte{})
	if err == nil {
		t.Fatal("expected error for empty slice, got nil")
	}
}

func TestQueryEvent_MaxLengthQuery(t *testing.T) {
	maxQuery := strings.Repeat("A", 4096)
	data := buildQueryEventBytes(1, 1, 3, maxQuery)

	ev, err := DecodeEvent(data)
	if err != nil {
		t.Fatalf("DecodeEvent returned unexpected error: %v", err)
	}

	qe := ev.(*QueryEvent)
	if qe.QueryLen != 4096 {
		t.Errorf("QueryLen = %d, want 4096", qe.QueryLen)
	}
	if qe.QueryString() != maxQuery {
		t.Errorf("QueryString() length = %d, want 4096", len(qe.QueryString()))
	}
}

func TestQueryEvent_ZeroLengthQuery(t *testing.T) {
	data := buildQueryEventBytes(1, 1, 3, "")

	ev, err := DecodeEvent(data)
	if err != nil {
		t.Fatalf("DecodeEvent returned unexpected error: %v", err)
	}

	qe := ev.(*QueryEvent)
	if qe.QueryLen != 0 {
		t.Errorf("QueryLen = %d, want 0", qe.QueryLen)
	}
	if qe.QueryString() != "" {
		t.Errorf("QueryString() = %q, want empty string", qe.QueryString())
	}
}

func TestQueryEvent_QueryStringTrimsToQueryLen(t *testing.T) {
	// Build a query event where the buffer has data beyond QueryLen.
	query := "SELECT 1"
	data := buildQueryEventBytes(1, 1, 3, query)

	// Manually poke extra non-zero bytes past the query text in the buffer.
	// The query buffer starts at offset 1 (type) + 8 (ts) + 4 (tid) + 1 (cmd) + 2 (qlen) = 16
	data[16+len(query)] = 'X'
	data[16+len(query)+1] = 'Y'

	ev, err := DecodeEvent(data)
	if err != nil {
		t.Fatalf("DecodeEvent returned unexpected error: %v", err)
	}

	qe := ev.(*QueryEvent)
	if qe.QueryString() != query {
		t.Errorf("QueryString() = %q, want %q", qe.QueryString(), query)
	}
}

func TestDecodeEvent_LittleEndianByteOrder(t *testing.T) {
	// Manually construct a latency event with known byte pattern.
	// timestamp_ns = 0x0102030405060708 → LE bytes: 08 07 06 05 04 03 02 01
	// tid          = 0x0A0B0C0D         → LE bytes: 0D 0C 0B 0A
	// latency_ns   = 0x1112131415161718 → LE bytes: 18 17 16 15 14 13 12 11
	data := []byte{
		EventTypeLatency,
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, // timestamp LE
		0x0D, 0x0C, 0x0B, 0x0A, // tid LE
		0x18, 0x17, 0x16, 0x15, 0x14, 0x13, 0x12, 0x11, // latency LE
	}

	ev, err := DecodeEvent(data)
	if err != nil {
		t.Fatalf("DecodeEvent returned unexpected error: %v", err)
	}

	le := ev.(*LatencyEvent)
	if le.TimestampNs != 0x0102030405060708 {
		t.Errorf("TimestampNs = 0x%016X, want 0x0102030405060708", le.TimestampNs)
	}
	if le.TID != 0x0A0B0C0D {
		t.Errorf("TID = 0x%08X, want 0x0A0B0C0D", le.TID)
	}
	if le.LatencyNs != 0x1112131415161718 {
		t.Errorf("LatencyNs = 0x%016X, want 0x1112131415161718", le.LatencyNs)
	}
}
