package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

var update = flag.Bool("update", false, "regenerate CLI goldens under cmd/llm-lint/testdata/")

// captureStdout pipes os.Stdout for the duration of fn and returns whatever
// was written. Not parallel-safe (global os.Stdout) — tests using it run
// sequentially under stdoutMu.
var stdoutMu sync.Mutex

func captureStdout(t *testing.T, fn func()) []byte {
	t.Helper()
	stdoutMu.Lock()
	defer stdoutMu.Unlock()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	done := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()

	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

func runCommand(t *testing.T, args ...string) ([]byte, []byte) {
	t.Helper()
	cmd := newRoot()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	stdoutBytes := captureStdout(t, func() {
		_ = cmd.Execute()
	})

	// help/usage text comes via cobra (SetOut/SetErr); program output
	// (version, rules list, rules show) comes via os.Stdout. Concatenate
	// so the golden captures whichever path the command uses.
	combined := append([]byte{}, outBuf.Bytes()...)
	combined = append(combined, stdoutBytes...)
	return combined, errBuf.Bytes()
}

func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `go test ./cmd/llm-lint/... -update` to create it)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s mismatch.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestCLI_Help(t *testing.T) {
	out, _ := runCommand(t, "--help")
	compareGolden(t, "help.golden", out)
}

func TestCLI_ScanHelp(t *testing.T) {
	out, _ := runCommand(t, "scan", "--help")
	compareGolden(t, "scan_help.golden", out)
}

func TestCLI_RulesList(t *testing.T) {
	out, _ := runCommand(t, "rules")
	// Confirm every documented rule appears in the listing — guards against
	// silent rule de-registration regressions (`init()` import side-effect issues).
	for _, id := range []string{
		"LLM001", "LLM002", "LLM003", "LLM004", "LLM005",
		"LLM006", "LLM007", "LLM008", "LLM009", "LLM010",
		"LLM011", "LLM012", "LLM013", "LLM014", "LLM015",
	} {
		if !strings.Contains(string(out), id) {
			t.Errorf("rules list missing %s\n--- output ---\n%s", id, out)
		}
	}
	compareGolden(t, "rules_list.golden", out)
}

func TestCLI_RulesShow_LLM003(t *testing.T) {
	out, _ := runCommand(t, "rules", "show", "LLM003")
	for _, want := range []string{
		"LLM003",
		"Co-authored-by",
		"includeCoAuthoredBy",
		"~/.claude/settings.json",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("rules show LLM003 missing %q\n--- output ---\n%s", want, out)
		}
	}
	compareGolden(t, "rules_show_llm003.golden", out)
}

func TestCLI_RulesShow_UnknownID(t *testing.T) {
	cmd := newRoot()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"rules", "show", "LLM999"})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for unknown rule ID")
	}
	if err != nil && !strings.Contains(err.Error(), "unknown rule") {
		t.Errorf("expected 'unknown rule' in error; got %v", err)
	}
}

func TestCLI_Version(t *testing.T) {
	out, _ := runCommand(t, "version")
	// Version is `dev` in test builds; the binary prints it followed by a newline.
	versionRe := regexp.MustCompile(`^[^\s]+\n$`)
	if !versionRe.Match(out) {
		t.Errorf("version output not single token+newline: %q", out)
	}
	if !strings.Contains(string(out), "dev") {
		t.Errorf("test binary should print version 'dev'; got %q", out)
	}
}

func TestCLI_UnknownCommand_Errors(t *testing.T) {
	cmd := newRoot()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"frobnicate"})
	if err := cmd.Execute(); err == nil {
		t.Error("expected error for unknown subcommand")
	}
}
