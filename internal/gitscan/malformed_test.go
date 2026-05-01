package gitscan_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/gitscan"
	"github.com/JadenRazo/llm-lint/internal/rules"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

// Malformed-repo cases used to be a source of panics in shells/wrappers that
// blindly trusted the .git layout. The scanner must return an error (or an
// empty result) without panicking on these.

func TestGitScan_MalformedHEAD_NoPanic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Empty HEAD: go-git typically errors on Open, which we wrap and surface.
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &testCfg{depth: 100}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("gitscan must not panic on malformed HEAD; got panic: %v", r)
		}
	}()
	_, _ = s.Scan(root) // either err or empty result is fine; panic is not.
}

func TestGitScan_GitDirIsAFile_GracefulError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// .git as a regular file (e.g. submodule gitfile) with garbage content.
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &testCfg{depth: 100}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("gitscan must not panic when .git is a malformed gitfile; got panic: %v", r)
		}
	}()
	_, err := s.Scan(root)
	if err == nil {
		t.Skip("scanner accepted malformed .git file; either is acceptable, but documenting current behavior would be welcome")
	}
}

func TestGitScan_NoCommits_EmptyResult(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n\trepositoryformatversion = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &testCfg{depth: 100}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	res, err := s.Scan(root)
	if err != nil {
		// An error here is acceptable — go-git's behavior on a bare .git skeleton
		// is implementation-defined. The contract we care about is "no panic."
		t.Logf("ok: scanner errored gracefully: %v", err)
		return
	}
	if res == nil {
		t.Error("Scan must not return (nil, nil)")
		return
	}
	if len(res.Matches) != 0 {
		t.Errorf("empty repo should have no matches; got %d", len(res.Matches))
	}
}
