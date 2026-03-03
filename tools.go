//go:build tools

package tools

// tools.go declares tool and platform-gated dependencies that must remain
// in go.mod even when the importing source files are excluded by build tags
// on the current platform (e.g., cilium/ebpf is used only in linux-tagged
// files but must be resolvable in go.mod for CI).
import (
	_ "github.com/cilium/ebpf"
	_ "github.com/cilium/ebpf/link"
	_ "github.com/cilium/ebpf/ringbuf"
)
