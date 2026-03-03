// Package ebpf handles loading and attaching BPF programs to mysqld uprobes.
package ebpf

import (
	"encoding/binary"
	"fmt"
)

// Event type constants identify the BPF ring-buffer record type.
const (
	EventTypeQuery   uint8 = 1
	EventTypeLatency uint8 = 2
)

// queryEventSize is the byte length of a QueryEvent after the type prefix.
const queryEventSize = 8 + 4 + 1 + 2 + 4096 // 4111

// latencyEventSize is the byte length of a LatencyEvent after the type prefix.
const latencyEventSize = 8 + 4 + 8 // 20

// Event is the interface for all BPF events read from the ring buffer.
type Event interface {
	EventType() uint8
}

// QueryEvent is emitted by the uprobe on dispatch_command entry.
type QueryEvent struct {
	TimestampNs uint64     // ktime_ns
	TID         uint32     // kernel thread ID
	Command     uint8      // enum_server_command (COM_QUERY=3, COM_STMT_PREPARE=22, etc.)
	QueryLen    uint16     // actual query length (may be < cap(Query))
	Query       [4096]byte // raw SQL including comments, null-padded
}

// EventType returns EventTypeQuery.
func (e *QueryEvent) EventType() uint8 { return EventTypeQuery }

// QueryString returns the query text as a Go string, trimmed to QueryLen.
func (e *QueryEvent) QueryString() string {
	n := int(e.QueryLen)
	if n > len(e.Query) {
		n = len(e.Query)
	}
	return string(e.Query[:n])
}

// LatencyEvent is emitted by the uretprobe on dispatch_command return.
type LatencyEvent struct {
	TimestampNs uint64 // ktime_ns (return time)
	TID         uint32 // kernel thread ID (matches QueryEvent.TID)
	LatencyNs   uint64 // uretprobe_ts - uprobe_ts
}

// EventType returns EventTypeLatency.
func (e *LatencyEvent) EventType() uint8 { return EventTypeLatency }

// DecodeEvent decodes a raw ring-buffer record into a typed Event.
// The first byte identifies the event type; the remaining bytes are the
// packed, little-endian event struct.
func DecodeEvent(data []byte) (Event, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty event data")
	}

	eventType := data[0]
	payload := data[1:]

	switch eventType {
	case EventTypeQuery:
		return decodeQueryEvent(payload)
	case EventTypeLatency:
		return decodeLatencyEvent(payload)
	default:
		return nil, fmt.Errorf("unknown event type: %d", eventType)
	}
}

func decodeQueryEvent(data []byte) (*QueryEvent, error) {
	if len(data) < queryEventSize {
		return nil, fmt.Errorf("query event too short: got %d bytes, need %d", len(data), queryEventSize)
	}

	e := &QueryEvent{
		TimestampNs: binary.LittleEndian.Uint64(data[0:8]),
		TID:         binary.LittleEndian.Uint32(data[8:12]),
		Command:     data[12],
		QueryLen:    binary.LittleEndian.Uint16(data[13:15]),
	}
	copy(e.Query[:], data[15:15+4096])

	return e, nil
}

func decodeLatencyEvent(data []byte) (*LatencyEvent, error) {
	if len(data) < latencyEventSize {
		return nil, fmt.Errorf("latency event too short: got %d bytes, need %d", len(data), latencyEventSize)
	}

	e := &LatencyEvent{
		TimestampNs: binary.LittleEndian.Uint64(data[0:8]),
		TID:         binary.LittleEndian.Uint32(data[8:12]),
		LatencyNs:   binary.LittleEndian.Uint64(data[12:20]),
	}

	return e, nil
}
