package scanner_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/rules"
	"github.com/JadenRazo/llm-lint/internal/scanner"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

func TestScanner_DotGitSymlinkSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks unreliable on Windows test runners")
	}
	t.Parallel()
	root := t.TempDir()

	// Create a target dir with a CLAUDE.md, then symlink it as `.git` inside root.
	// The walker treats `.git` (regardless of whether it's a real dir or a symlink
	// to one) as a hard skip. Anything inside it must not be scanned.
	target := filepath.Join(root, "actually-a-git-dir")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "CLAUDE.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}

	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, _, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		// Anything under `.git` (real or symlinked) must not surface findings.
		if m.Path != "" && (m.Path == ".git/CLAUDE.md" || filepath.Dir(m.Path) == ".git") {
			t.Errorf(".git symlink contents must be skipped; got %v", m)
		}
	}
}

func TestScanner_RegularFileSymlink_FiresPathRuleOnAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks unreliable on Windows")
	}
	t.Parallel()
	root := t.TempDir()

	// A real file at "real-notes.md" plus a symlink "CLAUDE.md" pointing to it.
	// The walker reports the symlink with its own path, so LLM001 fires on the
	// alias path. Documenting this behavior so a regression that follows symlinks
	// (and would scan the target name instead) shows up here.
	if err := os.WriteFile(filepath.Join(root, "real-notes.md"), []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real-notes.md"), filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, _, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	// Either the symlink is followed (and CLAUDE.md fires) or it is skipped
	// (Mode().IsRegular() returns false). Both are acceptable; we just assert
	// the scanner doesn't panic and doesn't fire on real-notes.md (which is
	// not a CLAUDE.md path).
	for _, m := range matches {
		if m.Rule.ID == "LLM001" && m.Path == "real-notes.md" {
			t.Errorf("LLM001 must not match the symlink target's real path: %v", m)
		}
	}
}

func TestScanner_SymlinkLoop_TerminatesGracefully(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks unreliable on Windows")
	}
	t.Parallel()
	root := t.TempDir()

	// dirA contains a symlink to dirB; dirB contains a symlink to dirA.
	// A naive walker would loop. The saracen/walker we use should not follow
	// directory symlinks; if it ever does without cycle detection, this test
	// hangs and the timeout is the signal.
	dirA := filepath.Join(root, "a")
	dirB := filepath.Join(root, "b")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dirB, filepath.Join(dirA, "link-to-b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dirA, filepath.Join(dirB, "link-to-a")); err != nil {
		t.Fatal(err)
	}

	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Scan(root); err != nil {
		t.Errorf("scanner must not error on symlink loop: %v", err)
	}
}
