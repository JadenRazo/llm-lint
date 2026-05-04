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

	"github.com/JadenRazo/llm-lint/internal/config"
	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/rules"

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

// TestFailOn_FlagDefaultIsEmpty pins the cobra default for --fail-on to
// "" (empty). This is the contract the resolution chain in runScan
// depends on — if cobra's default were "error" again, runScan couldn't
// distinguish "user passed --fail-on error" from "user passed nothing"
// and the config-file fail_on would be silently overridden. The
// resolution-chain test below assumes this default; this test pins it.
func TestFailOn_FlagDefaultIsEmpty(t *testing.T) {
	cmd := newScanCmd()
	got, err := cmd.Flags().GetString("fail-on")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("--fail-on default must be \"\" so cfg.FailOn can win when the user omits the flag; got %q", got)
	}
}

// TestFailOn_ConfigFileHonoredWhenFlagAbsent is a regression for the bug
// where the cobra default of "error" for --fail-on always won over the
// config-file fail_on. We don't drive the full CLI (runScan calls
// os.Exit(1) on threshold breach, which would kill the test process);
// instead we cover the resolution chain at the level where the bug lived:
//
//  1. config.Load reads `fail_on: warning` from .llmlint.yaml into cfg.FailOn.
//  2. The resolution rule (CLI flag if non-empty, else cfg.FailOn) yields "warning".
//  3. engine.ExceedsThreshold("warning") trips on a warning-level finding.
//
// Before the fix, step 2 always produced "error" (since the cobra default
// was "error", indistinguishable from "user passed error"), and step 3
// didn't trip — so a warning-only repo silently exited 0.
func TestFailOn_ConfigFileHonoredWhenFlagAbsent(t *testing.T) {
	root := t.TempDir()

	// CLAUDE.md fires LLM001 (warning severity) — typical "dirty repo" finding.
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yamlBody := "version: 1\nfail_on: warning\nscan:\n  git_history: false\n"
	if err := os.WriteFile(filepath.Join(root, ".llmlint.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(".llmlint.yaml", root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.FailOn != rules.SevWarning {
		t.Fatalf("config.Load did not parse fail_on: warning; got %q", cfg.FailOn)
	}

	// Mirror runScan's resolution: empty CLI flag -> fall through to cfg.FailOn.
	flagValue := ""
	failOn := flagValue
	if failOn == "" {
		failOn = string(cfg.FailOn)
	}
	if failOn != "warning" {
		t.Fatalf("resolved fail-on should be %q, got %q", "warning", failOn)
	}
	if err := engine.ValidateFailOn(failOn); err != nil {
		t.Fatalf("ValidateFailOn(%q) returned error: %v", failOn, err)
	}

	// Drive the gate against a synthetic warning-severity finding (the real
	// LLM001 rule is severity=warning, so this matches what a full scan
	// would produce against the dirty CLAUDE.md fixture above).
	res := &engine.Result{Findings: []findings.Finding{{
		RuleID:   "LLM001",
		Severity: rules.SevWarning,
		Location: findings.Location{Kind: findings.LocFile, Path: "CLAUDE.md"},
	}}}
	if !engine.ExceedsThreshold(res, failOn) {
		t.Errorf("ExceedsThreshold(warning) should trip on a warning finding when config sets fail_on: warning")
	}

	// And confirm the pre-fix behavior would have missed it: had the CLI
	// flag default of "error" been preserved, the same warning finding
	// would NOT exceed the threshold. Pinning this avoids a future
	// "let's just default to error again" refactor silently re-breaking it.
	if engine.ExceedsThreshold(res, "error") {
		t.Errorf("sanity: warning finding must not exceed error threshold (pre-fix behavior)")
	}
}

// TestFailOn_InvalidValueRejected is a regression for the second bug:
// `--fail-on garbge` flowed into ExceedsThreshold where unknown strings
// rank as 0, silently making the gate trip on every finding. The fix
// validates the resolved value via engine.ValidateFailOn and surfaces the
// error before os.Exit(1) is reached, so cobra's RunE error path returns it.
func TestFailOn_InvalidValueRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newRoot()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"scan", "--fail-on", "garbge", "--no-git", root})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --fail-on value")
	}
	msg := err.Error()
	if !strings.Contains(msg, "invalid --fail-on") {
		t.Errorf("error %q missing prefix %q", msg, "invalid --fail-on")
	}
	if !strings.Contains(msg, "garbge") {
		t.Errorf("error %q missing offending value %q", msg, "garbge")
	}
}

func TestFix_RejectsStagedOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newRoot()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"scan", "--fix", "--staged-only", "--no-git", root})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --fix with --staged-only")
	}
	if !strings.Contains(err.Error(), "--fix cannot be used with --staged-only") {
		t.Fatalf("unexpected error: %v", err)
	}
}
