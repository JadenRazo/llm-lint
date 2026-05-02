package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

// runBaseline executes a baseline subcommand against newRoot() and returns
// stdout (cobra writer) plus error. captureStdout is shared with main_test.go.
func runBaseline(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	cmd := newRoot()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return outBuf.Bytes(), err
}

// makeDirtyRepo creates a temp dir with one CLAUDE.md (LLM001) and a config
// that disables the git-history scanner so the test stays fast and doesn't
// require a real git repo for the trailer scanner.
func makeDirtyRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := "version: 1\nscan:\n  git_history: false\n"
	if err := os.WriteFile(filepath.Join(root, ".llmlint.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCLI_BaselineCreate_WritesFile(t *testing.T) {
	root := makeDirtyRepo(t)
	out, err := runBaseline(t, "baseline", "create", "--path", root)
	if err != nil {
		t.Fatalf("baseline create: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "wrote") || !strings.Contains(string(out), "1 entries") {
		t.Errorf("expected 'wrote ... 1 entries' in stdout; got %q", out)
	}
	bp := filepath.Join(root, ".llmlint-baseline.yaml")
	body, err := os.ReadFile(bp)
	if err != nil {
		t.Fatalf("baseline file not written: %v", err)
	}
	for _, want := range []string{"version: 1", "LLM001", "fingerprint:"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("baseline missing %q\n--- file ---\n%s", want, body)
		}
	}
}

func TestCLI_BaselineCreate_RefusesOverwriteWithoutForce(t *testing.T) {
	root := makeDirtyRepo(t)
	if _, err := runBaseline(t, "baseline", "create", "--path", root); err != nil {
		t.Fatal(err)
	}
	_, err := runBaseline(t, "baseline", "create", "--path", root)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error on second create; got %v", err)
	}
}

func TestCLI_BaselineUpdate_OverwritesExisting(t *testing.T) {
	root := makeDirtyRepo(t)
	if _, err := runBaseline(t, "baseline", "create", "--path", root); err != nil {
		t.Fatal(err)
	}
	out, err := runBaseline(t, "baseline", "update", "--path", root)
	if err != nil {
		t.Fatalf("baseline update: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "wrote") {
		t.Errorf("expected 'wrote' on update; got %q", out)
	}
}

func TestCLI_BaselineStatus_NoFile(t *testing.T) {
	root := makeDirtyRepo(t)
	out, err := runBaseline(t, "baseline", "status", "--path", root)
	if err != nil {
		t.Fatalf("baseline status: %v", err)
	}
	if !strings.Contains(string(out), "no baseline file") {
		t.Errorf("expected 'no baseline file' message; got %q", out)
	}
}

func TestCLI_BaselineStatus_AfterCreate(t *testing.T) {
	root := makeDirtyRepo(t)
	if _, err := runBaseline(t, "baseline", "create", "--path", root); err != nil {
		t.Fatal(err)
	}
	out, err := runBaseline(t, "baseline", "status", "--path", root)
	if err != nil {
		t.Fatalf("baseline status: %v\n%s", err, out)
	}
	for _, want := range []string{"baseline:", "matched", "1 findings already baselined", "new", "stale"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("status missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestCLI_BaselineStatus_DetectsNewAndStale(t *testing.T) {
	root := makeDirtyRepo(t)
	if _, err := runBaseline(t, "baseline", "create", "--path", root); err != nil {
		t.Fatal(err)
	}
	// Add a new finding that isn't in the baseline (.cursorrules → LLM006).
	if err := os.WriteFile(filepath.Join(root, ".cursorrules"), []byte("rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Remove the original finding to make the baseline stale.
	if err := os.Remove(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	out, err := runBaseline(t, "baseline", "status", "--path", root)
	if err != nil {
		t.Fatalf("baseline status: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "1 findings not in baseline") {
		t.Errorf("expected 1 new finding; got %q", out)
	}
	if !strings.Contains(string(out), "1 baseline entries no longer match") {
		t.Errorf("expected 1 stale entry; got %q", out)
	}
	if !strings.Contains(string(out), "baseline update") || !strings.Contains(string(out), "baseline prune") {
		t.Errorf("expected next-step hints to mention update + prune; got %q", out)
	}
}

func TestCLI_BaselinePrune_DropsStale(t *testing.T) {
	root := makeDirtyRepo(t)
	if _, err := runBaseline(t, "baseline", "create", "--path", root); err != nil {
		t.Fatal(err)
	}
	// Make the baseline stale by removing the finding.
	if err := os.Remove(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	out, err := runBaseline(t, "baseline", "prune", "--path", root)
	if err != nil {
		t.Fatalf("baseline prune: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "pruned 1 stale entries") {
		t.Errorf("expected 'pruned 1 stale entries'; got %q", out)
	}
	if !strings.Contains(string(out), "0 remain") {
		t.Errorf("expected '0 remain' post-prune; got %q", out)
	}
}

func TestCLI_BaselinePrune_NothingStale(t *testing.T) {
	root := makeDirtyRepo(t)
	if _, err := runBaseline(t, "baseline", "create", "--path", root); err != nil {
		t.Fatal(err)
	}
	out, err := runBaseline(t, "baseline", "prune", "--path", root)
	if err != nil {
		t.Fatalf("baseline prune: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "no stale entries") {
		t.Errorf("expected 'no stale entries' message; got %q", out)
	}
}

func TestCLI_BaselinePrune_NoBaselineFile(t *testing.T) {
	root := makeDirtyRepo(t)
	_, err := runBaseline(t, "baseline", "prune", "--path", root)
	if err == nil || !strings.Contains(err.Error(), "no baseline file") {
		t.Errorf("expected 'no baseline file' error; got %v", err)
	}
}

func TestCLI_BaselineCreate_HonorsExclude(t *testing.T) {
	root := makeDirtyRepo(t)
	out, err := runBaseline(t, "baseline", "create", "--path", root, "--exclude", "LLM001")
	if err != nil {
		t.Fatalf("baseline create: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "0 entries") {
		t.Errorf("expected 0 entries when LLM001 excluded; got %q", out)
	}
}
