package fixer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/config"
	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/fixer"
	"github.com/JadenRazo/llm-lint/internal/rules"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

func TestApply_RemovesContentLines(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(src, "handler.py")
	body := "def f():\n    # I'm sorry, but I can't help with that.\n    return 1\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res := scan(t, root)
	summary, err := fixer.Apply(root, res.Findings, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if summary.LinesRemoved != 1 || summary.FilesChanged != 1 {
		t.Fatalf("summary = %+v, want one line removed in one file", summary)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "I'm sorry") {
		t.Fatalf("auto-fix left refusal text behind:\n%s", got)
	}
	if len(scan(t, root).Findings) != 0 {
		t.Fatalf("expected clean rescan after content fix")
	}
}

func TestApply_IgnoresAndUntracksPathFindings(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "CLAUDE.md")

	res := scan(t, root)
	summary, err := fixer.Apply(root, res.Findings, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if summary.GitignoreAdded != 1 || summary.IndexEntriesFixed != 1 {
		t.Fatalf("summary = %+v, want one gitignore entry and one untracked index entry", summary)
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "CLAUDE.md\n") {
		t.Fatalf(".gitignore missing CLAUDE.md:\n%s", ignore)
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatalf("CLAUDE.md should remain in working tree: %v", err)
	}
	out := runGit(t, root, "ls-files")
	if strings.Contains(out, "CLAUDE.md") {
		t.Fatalf("CLAUDE.md should be removed from the index, got:\n%s", out)
	}
}

func scan(t *testing.T, root string) *engine.Result {
	t.Helper()
	cfg, err := config.Load(".llmlint.yaml", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ApplyCLIOverrides(config.CLIOverrides{NoGit: true, NoBaseline: true}); err != nil {
		t.Fatal(err)
	}
	res, err := engine.New(rules.DefaultRegistry(), cfg).Run(root)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
