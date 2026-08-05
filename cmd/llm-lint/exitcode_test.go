package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// execScan runs a scan through the real cobra command tree and returns the
// error, letting tests assert exit codes via exitCodeError. This is only
// possible because runScan returns typed errors instead of calling os.Exit.
func execScan(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newRoot()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	var err error
	_ = captureStdout(t, func() { err = cmd.Execute() })
	return err
}

func TestScan_ExitCode_CleanRepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := execScan(t, "scan", dir, "--no-git", "--no-progress"); err != nil {
		t.Fatalf("clean repo should exit 0, got %v", err)
	}
}

func TestScan_ExitCode_ThresholdBreach(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := execScan(t, "scan", dir, "--no-git", "--no-progress")
	var ec *exitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("want exitCodeError, got %v", err)
	}
	if ec.code != exitThreshold {
		t.Errorf("want exit code %d, got %d", exitThreshold, ec.code)
	}
}

func TestScan_ExitCode_InvalidFailOnBeforeReport(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.json")
	if err := os.WriteFile(out, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := execScan(t, "scan", dir, "--no-git", "--no-progress", "--fail-on", "bogus", "--format", "json", "--output", out)
	if err == nil {
		t.Fatal("invalid --fail-on should error")
	}
	var ec *exitCodeError
	if errors.As(err, &ec) {
		t.Fatalf("invalid --fail-on should be a plain error (exit 2), got exitCodeError %d", ec.code)
	}
	// fail-on is validated before scanning/reporting, so the output file
	// must be untouched.
	data, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "precious" {
		t.Errorf("output file was clobbered before validation: %q", data)
	}
}

func TestScan_ExitCode_UnknownFormatDoesNotTruncateOutput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.sarif")
	if err := os.WriteFile(out, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := execScan(t, "scan", dir, "--no-git", "--no-progress", "--format", "bogus", "--output", out)
	if err == nil {
		t.Fatal("unknown format should error")
	}
	data, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "precious" {
		t.Errorf("unknown --format truncated the output file: %q", data)
	}
}
