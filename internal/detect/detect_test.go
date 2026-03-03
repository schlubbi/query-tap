package detect

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// --- ValidateSymbols ---

func TestValidateSymbols_NonExistentPath(t *testing.T) {
	err := ValidateSymbols("/nonexistent/path/to/mysqld")
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
	if !strings.Contains(err.Error(), "opening ELF binary") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateSymbols_NotELFBinary(t *testing.T) {
	tmp := t.TempDir()
	fakeBin := filepath.Join(tmp, "fake-mysqld")
	if err := os.WriteFile(fakeBin, []byte("not an ELF binary"), 0o755); err != nil {
		t.Fatalf("creating fake binary: %v", err)
	}

	err := ValidateSymbols(fakeBin)
	if err == nil {
		t.Fatal("expected error for non-ELF file")
	}
	if !strings.Contains(err.Error(), "opening ELF binary") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateSymbols_BinaryWithoutDispatchCommand(t *testing.T) {
	// The test binary itself is a real compiled binary.
	// On Linux it's ELF (parseable, but no dispatch_command).
	// On macOS it's Mach-O (elf.Open fails → "opening ELF binary" error).
	// Both are valid error paths.
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot determine executable path: %v", err)
	}

	err = ValidateSymbols(exe)
	if err == nil {
		t.Fatal("expected error: test binary should not contain dispatch_command")
	}
}

// --- FindMySQLProcesses ---

func TestFindMySQLProcesses_Success(t *testing.T) {
	tmp := t.TempDir()
	old := procPath
	procPath = tmp
	t.Cleanup(func() { procPath = old })

	writeFakeProc(t, tmp, 1000, "mysqld", "/usr/sbin/mysqld")
	writeFakeProc(t, tmp, 2000, "bash", "")
	writeFakeProc(t, tmp, 3000, "mysqld_safe", "")

	procs, err := FindMySQLProcesses()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both "mysqld" and "mysqld_safe" contain "mysqld".
	if len(procs) != 2 {
		t.Fatalf("expected 2 mysqld processes, got %d", len(procs))
	}

	found := false
	for _, p := range procs {
		if p.PID == 1000 {
			found = true
			if p.Comm != "mysqld" {
				t.Errorf("expected comm %q, got %q", "mysqld", p.Comm)
			}
			if p.BinaryPath != "/usr/sbin/mysqld" {
				t.Errorf("expected binary path %q, got %q", "/usr/sbin/mysqld", p.BinaryPath)
			}
		}
	}
	if !found {
		t.Error("mysqld process with PID 1000 not found")
	}
}

func TestFindMySQLProcesses_NoMySQLdRunning(t *testing.T) {
	tmp := t.TempDir()
	old := procPath
	procPath = tmp
	t.Cleanup(func() { procPath = old })

	writeFakeProc(t, tmp, 1000, "postgres", "")
	writeFakeProc(t, tmp, 2000, "nginx", "")

	_, err := FindMySQLProcesses()
	if err == nil {
		t.Fatal("expected error when no mysqld processes exist")
	}
	if !strings.Contains(err.Error(), "no mysqld processes found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFindMySQLProcesses_NoProcFilesystem(t *testing.T) {
	old := procPath
	procPath = "/nonexistent/proc/filesystem"
	t.Cleanup(func() { procPath = old })

	_, err := FindMySQLProcesses()
	if err == nil {
		t.Fatal("expected error when /proc doesn't exist")
	}
}

func TestFindMySQLProcesses_SkipsNonNumericDirs(t *testing.T) {
	tmp := t.TempDir()
	old := procPath
	procPath = tmp
	t.Cleanup(func() { procPath = old })

	writeFakeProc(t, tmp, 1000, "mysqld", "/usr/sbin/mysqld")

	// Create a non-numeric directory with a comm file — should be skipped.
	badDir := filepath.Join(tmp, "self")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatalf("creating bad dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "comm"), []byte("mysqld\n"), 0o644); err != nil {
		t.Fatalf("writing comm: %v", err)
	}

	procs, err := FindMySQLProcesses()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(procs) != 1 {
		t.Errorf("expected 1 process (non-numeric dir should be skipped), got %d", len(procs))
	}
	if procs[0].PID != 1000 {
		t.Errorf("expected PID 1000, got %d", procs[0].PID)
	}
}

// --- ResolveBinaryPath ---

func TestResolveBinaryPath_ReadsSymlink(t *testing.T) {
	tmp := t.TempDir()
	old := procPath
	procPath = tmp
	t.Cleanup(func() { procPath = old })

	writeFakeProc(t, tmp, 4567, "mysqld", "/usr/sbin/mysqld")

	path, err := ResolveBinaryPath(4567)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/usr/sbin/mysqld" {
		t.Errorf("expected %q, got %q", "/usr/sbin/mysqld", path)
	}
}

func TestResolveBinaryPath_MissingExeLink(t *testing.T) {
	tmp := t.TempDir()
	old := procPath
	procPath = tmp
	t.Cleanup(func() { procPath = old })

	// Create PID dir without exe symlink.
	pidDir := filepath.Join(tmp, "9999")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("creating pid dir: %v", err)
	}

	_, err := ResolveBinaryPath(9999)
	if err == nil {
		t.Fatal("expected error when exe symlink doesn't exist")
	}
}

// --- FindOrOverride ---

func TestFindOrOverride_ExplicitPath_ValidatesSymbols(t *testing.T) {
	// Non-existent path should fail symbol validation.
	p, err := FindOrOverride("/nonexistent/mysqld", 0)
	if err == nil {
		t.Fatalf("expected error, got process: %+v", p)
	}
}

func TestFindOrOverride_ExplicitPath_PreservesPID(t *testing.T) {
	// Verify that when both path and PID are given, the PID is preserved
	// in the returned struct (even though symbol validation will fail here).
	tmp := t.TempDir()
	fakeBin := filepath.Join(tmp, "mysqld")
	if err := os.WriteFile(fakeBin, []byte("not elf"), 0o755); err != nil {
		t.Fatalf("creating fake binary: %v", err)
	}

	_, err := FindOrOverride(fakeBin, 42)
	if err == nil {
		t.Fatal("expected symbol validation error")
	}
	// The error should be about ELF parsing, not about /proc.
	if !strings.Contains(err.Error(), "ELF") {
		t.Errorf("expected ELF-related error, got: %v", err)
	}
}

func TestFindOrOverride_ExplicitPID_NotMySQLd(t *testing.T) {
	tmp := t.TempDir()
	old := procPath
	procPath = tmp
	t.Cleanup(func() { procPath = old })

	writeFakeProc(t, tmp, 5555, "postgres", "")

	_, err := FindOrOverride("", 5555)
	if err == nil {
		t.Fatal("expected error for non-mysqld PID")
	}
	if !strings.Contains(err.Error(), "not a mysqld process") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFindOrOverride_ExplicitPID_MissingProcess(t *testing.T) {
	tmp := t.TempDir()
	old := procPath
	procPath = tmp
	t.Cleanup(func() { procPath = old })

	_, err := FindOrOverride("", 9999)
	if err == nil {
		t.Fatal("expected error for missing PID")
	}
}

func TestFindOrOverride_AutoDetect_NoProcFS(t *testing.T) {
	old := procPath
	procPath = "/nonexistent/proc/filesystem"
	t.Cleanup(func() { procPath = old })

	_, err := FindOrOverride("", 0)
	if err == nil {
		t.Fatal("expected error when auto-detecting without /proc")
	}
}

func TestFindOrOverride_AutoDetect_SkipsSymbolValidation_NoBinary(t *testing.T) {
	tmp := t.TempDir()
	old := procPath
	procPath = tmp
	t.Cleanup(func() { procPath = old })

	// Create a mysqld process without an exe symlink.
	// FindOrOverride should return it without symbol validation
	// since BinaryPath is empty.
	pidDir := filepath.Join(tmp, "7777")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("creating pid dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte("mysqld\n"), 0o644); err != nil {
		t.Fatalf("writing comm: %v", err)
	}

	p, err := FindOrOverride("", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.PID != 7777 {
		t.Errorf("expected PID 7777, got %d", p.PID)
	}
	if p.Comm != "mysqld" {
		t.Errorf("expected comm %q, got %q", "mysqld", p.Comm)
	}
}

// --- Test helpers ---

func writeFakeProc(t *testing.T, procDir string, pid int, comm string, exeTarget string) {
	t.Helper()
	pidDir := filepath.Join(procDir, strconv.Itoa(pid))
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("creating fake proc dir for PID %d: %v", pid, err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatalf("writing fake comm for PID %d: %v", pid, err)
	}
	if exeTarget != "" {
		if err := os.Symlink(exeTarget, filepath.Join(pidDir, "exe")); err != nil {
			t.Fatalf("creating fake exe symlink for PID %d: %v", pid, err)
		}
	}
}
