package fixer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestApply_PreviewDoesNotChangeContentLines(t *testing.T) {
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
	summary, err := fixer.ApplyWithOptions(root, res.Findings, rules.DefaultRegistry(), fixer.Options{
		Preview: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.LinesRemoved != 1 || summary.FilesChanged != 1 {
		t.Fatalf("summary = %+v, want preview of one line removed in one file", summary)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("preview changed file:\n%s", got)
	}
}

func TestApply_RemovesContentLinesPreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable mode bit is not reliable on Windows")
	}
	root := t.TempDir()
	src := filepath.Join(root, "scripts")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(src, "clean.sh")
	body := "#!/bin/sh\n# As an AI language model, I cannot do that\nexit 0\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	res := scan(t, root)
	if _, err := fixer.Apply(root, res.Findings, rules.DefaultRegistry()); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o755 {
		t.Fatalf("fixed file mode = %o, want 755", got)
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

func TestApply_PreviewDoesNotUntrackPathFindings(t *testing.T) {
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
	summary, err := fixer.ApplyWithOptions(root, res.Findings, rules.DefaultRegistry(), fixer.Options{
		Preview: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.GitignoreAdded != 1 || summary.IndexEntriesFixed != 1 {
		t.Fatalf("summary = %+v, want preview of one gitignore entry and one untracked index entry", summary)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("preview should not create .gitignore, err=%v", err)
	}
	out := runGit(t, root, "ls-files")
	if !strings.Contains(out, "CLAUDE.md") {
		t.Fatalf("preview should leave CLAUDE.md in the index, got:\n%s", out)
	}
}

func TestApply_CleansHeadCommitTrailer(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "feat: demo", "-m", "Co-authored-by: Claude <noreply@anthropic.com>")

	res := scanWithGit(t, root)
	if len(res.Findings) != 1 || res.Findings[0].RuleID != "LLM003" {
		t.Fatalf("expected one LLM003 finding, got %#v", res.Findings)
	}
	summary, err := fixer.Apply(root, res.Findings, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if summary.CommitMessages != 1 || summary.CommitLinesRemoved != 1 {
		t.Fatalf("summary = %+v, want one commit message cleaned and one line removed", summary)
	}
	msg := runGit(t, root, "log", "-1", "--format=%B")
	if strings.Contains(strings.ToLower(msg), "co-authored-by: claude") {
		t.Fatalf("auto-fix left Claude trailer behind:\n%s", msg)
	}
	if len(scanWithGit(t, root).Findings) != 0 {
		t.Fatalf("expected clean rescan after HEAD commit-message fix")
	}
}

func TestApply_PreviewDoesNotCleanHeadCommitTrailer(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "feat: demo", "-m", "Co-authored-by: Claude <noreply@anthropic.com>")
	oldHead := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	res := scanWithGit(t, root)
	summary, err := fixer.ApplyWithOptions(root, res.Findings, rules.DefaultRegistry(), fixer.Options{
		Preview: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.CommitMessages != 1 || summary.CommitLinesRemoved != 1 {
		t.Fatalf("summary = %+v, want preview of one commit message cleaned", summary)
	}
	if got := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD")); got != oldHead {
		t.Fatalf("preview changed HEAD: got %s want %s", got, oldHead)
	}
	msg := runGit(t, root, "log", "-1", "--format=%B")
	if !strings.Contains(strings.ToLower(msg), "co-authored-by: claude") {
		t.Fatalf("preview removed Claude trailer:\n%s", msg)
	}
}

func TestApply_CleansScannedCommitTrailers(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "feat: demo", "-m", "Co-authored-by: Claude <noreply@anthropic.com>")
	badCommit := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "main.go")
	runGit(t, root, "commit", "-m", "feat: app")
	oldHead := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	res := scanWithGit(t, root)
	if len(res.Findings) != 1 || res.Findings[0].Location.CommitSHA != badCommit {
		t.Fatalf("expected one finding on older commit %s, got %#v", badCommit, res.Findings)
	}
	summary, err := fixer.ApplyWithOptions(root, res.Findings, rules.DefaultRegistry(), fixer.Options{
		GitHistoryMode: string(fixer.GitHistoryScanned),
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.CommitMessages != 1 || summary.CommitLinesRemoved != 1 || summary.Unfixable != 0 {
		t.Fatalf("summary = %+v, want one historical commit cleaned", summary)
	}
	newHead := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	if newHead == oldHead {
		t.Fatal("expected HEAD to move after rewriting an older commit")
	}
	if msg := runGit(t, root, "log", "--format=%B"); strings.Contains(strings.ToLower(msg), "co-authored-by: claude") {
		t.Fatalf("auto-fix left Claude trailer in history:\n%s", msg)
	}
	if len(scanWithGit(t, root).Findings) != 0 {
		t.Fatalf("expected clean rescan after scanned history fix")
	}
}

func TestApply_PreviewDoesNotCleanScannedCommitTrailers(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "feat: demo", "-m", "Co-authored-by: Claude <noreply@anthropic.com>")

	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "main.go")
	runGit(t, root, "commit", "-m", "feat: app")
	oldHead := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	res := scanWithGit(t, root)
	summary, err := fixer.ApplyWithOptions(root, res.Findings, rules.DefaultRegistry(), fixer.Options{
		GitHistoryMode: string(fixer.GitHistoryScanned),
		Preview:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.CommitMessages != 1 || summary.CommitLinesRemoved != 1 {
		t.Fatalf("summary = %+v, want preview of one historical commit cleaned", summary)
	}
	if got := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD")); got != oldHead {
		t.Fatalf("preview changed HEAD: got %s want %s", got, oldHead)
	}
	if msg := runGit(t, root, "log", "--format=%B"); !strings.Contains(strings.ToLower(msg), "co-authored-by: claude") {
		t.Fatalf("preview removed Claude trailer from history:\n%s", msg)
	}
}

func TestApply_GitHistoryNoneLeavesCommitFindingsUnfixable(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "feat: demo", "-m", "Co-authored-by: Claude <noreply@anthropic.com>")
	oldHead := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	res := scanWithGit(t, root)
	summary, err := fixer.ApplyWithOptions(root, res.Findings, rules.DefaultRegistry(), fixer.Options{
		GitHistoryMode: string(fixer.GitHistoryNone),
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.CommitMessages != 0 || summary.Unfixable != 1 {
		t.Fatalf("summary = %+v, want commit finding left unfixable", summary)
	}
	if got := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD")); got != oldHead {
		t.Fatalf("HEAD changed in none mode: got %s want %s", got, oldHead)
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

func scanWithGit(t *testing.T, root string) *engine.Result {
	t.Helper()
	cfg, err := config.Load(".llmlint.yaml", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ApplyCLIOverrides(config.CLIOverrides{NoBaseline: true}); err != nil {
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
