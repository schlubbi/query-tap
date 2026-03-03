// Package detect implements mysqld process auto-detection.
//
// It scans /proc for running mysqld processes, resolves their binary paths,
// and validates that the binary has the dispatch_command symbol required
// for uprobe attachment.
package detect

import (
	"debug/elf"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procPath is the root of the proc filesystem. Overridable for testing.
var procPath = "/proc"

// MySQLProcess represents a discovered mysqld process.
type MySQLProcess struct {
	PID        int
	BinaryPath string // resolved from /proc/<pid>/exe
	Comm       string // from /proc/<pid>/comm
}

// FindMySQLProcesses scans /proc for running mysqld processes.
// Returns all discovered mysqld processes or an error if none are found.
func FindMySQLProcesses() ([]MySQLProcess, error) {
	pattern := filepath.Join(procPath, "[0-9]*", "comm")
	entries, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("scanning proc filesystem: %v", err)
	}

	var procs []MySQLProcess
	for _, commPath := range entries {
		data, err := os.ReadFile(commPath)
		if err != nil {
			// Permission denied or process exited — skip.
			continue
		}

		comm := strings.TrimSpace(string(data))
		if !strings.Contains(comm, "mysqld") {
			continue
		}

		dir := filepath.Dir(commPath)
		pidStr := filepath.Base(dir)
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		binaryPath, _ := ResolveBinaryPath(pid)

		procs = append(procs, MySQLProcess{
			PID:        pid,
			BinaryPath: binaryPath,
			Comm:       comm,
		})
	}

	if len(procs) == 0 {
		return nil, fmt.Errorf("no mysqld processes found")
	}
	return procs, nil
}

// ResolveBinaryPath reads /proc/<pid>/exe symlink to get the actual binary path.
func ResolveBinaryPath(pid int) (string, error) {
	exePath := filepath.Join(procPath, strconv.Itoa(pid), "exe")
	target, err := os.Readlink(exePath)
	if err != nil {
		return "", fmt.Errorf("resolving binary for PID %d: %v", pid, err)
	}
	// On Linux, upgraded/deleted binaries show a " (deleted)" suffix.
	target = strings.TrimSuffix(target, " (deleted)")
	return target, nil
}

// ValidateSymbols checks that the mysqld binary at the given path has the
// dispatch_command symbol needed for uprobe attachment. It inspects both
// .symtab and .dynsym sections via debug/elf.
func ValidateSymbols(binaryPath string) error {
	f, err := elf.Open(binaryPath)
	if err != nil {
		return fmt.Errorf("opening ELF binary %q: %v", binaryPath, err)
	}
	defer f.Close()

	if found, _ := findDispatchCommand(f.Symbols()); found {
		return nil
	}
	if found, _ := findDispatchCommand(f.DynamicSymbols()); found {
		return nil
	}

	return fmt.Errorf("symbol dispatch_command not found in %q: binary may be stripped", binaryPath)
}

// findDispatchCommand searches a symbol table for any symbol containing
// "dispatch_command". It accepts the multi-return from elf.File.Symbols()
// or DynamicSymbols() directly.
func findDispatchCommand(symbols []elf.Symbol, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	for _, sym := range symbols {
		if strings.Contains(sym.Name, "dispatch_command") {
			return true, nil
		}
	}
	return false, nil
}

// FindOrOverride returns a MySQLProcess based on the provided overrides:
//   - If mysqlPath is set, use it directly (skip /proc scan)
//   - If pid > 0, look up that specific PID in /proc
//   - Otherwise, auto-detect from /proc
//
// Returns an error if no mysqld is found or if symbol validation fails.
func FindOrOverride(mysqlPath string, pid int) (*MySQLProcess, error) {
	if mysqlPath != "" {
		if err := ValidateSymbols(mysqlPath); err != nil {
			return nil, err
		}
		return &MySQLProcess{
			PID:        pid,
			BinaryPath: mysqlPath,
		}, nil
	}

	if pid > 0 {
		return findByPID(pid)
	}

	procs, err := FindMySQLProcesses()
	if err != nil {
		return nil, err
	}

	p := &procs[0]
	if p.BinaryPath != "" {
		if err := ValidateSymbols(p.BinaryPath); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// findByPID looks up a specific PID in /proc and validates it is a mysqld process.
func findByPID(pid int) (*MySQLProcess, error) {
	commPath := filepath.Join(procPath, strconv.Itoa(pid), "comm")
	data, err := os.ReadFile(commPath)
	if err != nil {
		return nil, fmt.Errorf("reading process info for PID %d: %v", pid, err)
	}

	comm := strings.TrimSpace(string(data))
	if !strings.Contains(comm, "mysqld") {
		return nil, fmt.Errorf("PID %d is not a mysqld process (comm: %q)", pid, comm)
	}

	binaryPath, _ := ResolveBinaryPath(pid)

	p := &MySQLProcess{
		PID:        pid,
		BinaryPath: binaryPath,
		Comm:       comm,
	}

	if binaryPath != "" {
		if err := ValidateSymbols(binaryPath); err != nil {
			return nil, err
		}
	}

	return p, nil
}
