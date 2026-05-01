package gitscan_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/JadenRazo/llm-lint/internal/gitscan"
	"github.com/JadenRazo/llm-lint/internal/rules"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

// TestGitScan_SinceFlag_StopsAtBoundary verifies that --since (passed via
// Config.Since()) bounds the commit walk. The scanner stops *at* the named
// commit (exclusive of it), matching the documented "scan commits newer
// than this ref" semantics.
func TestGitScan_SinceFlag_StopsAtBoundary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	// Build 5 commits, the boundary in the middle gets a sentinel message.
	const total = 5
	hashes := make([]string, 0, total)
	for i := 0; i < total; i++ {
		fname := filepath.Join(root, "f"+itoa(i)+".txt")
		if err := os.WriteFile(fname, []byte(itoa(i)), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(filepath.Base(fname)); err != nil {
			t.Fatal(err)
		}
		h, err := wt.Commit("commit "+itoa(i)+"\n", &git.CommitOptions{
			Author: &object.Signature{
				Name: "Tester", Email: "t@example.com",
				When: time.Date(2026, 5, 1, 12, i, 0, 0, time.UTC),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, h.String())
	}

	// Boundary = the 2nd commit. Newer commits are 2,3,4 (i.e. 3 commits) when
	// walking from HEAD toward this boundary.
	boundary := hashes[1]

	cfg := &testCfg{depth: 100, since: boundary}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	res, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	want := total - 2 // exclude the boundary commit and everything older
	if res.CommitsScanned != want {
		t.Errorf("--since=%s: scanned %d commits, want %d", boundary[:7], res.CommitsScanned, want)
	}
}

func TestGitScan_SinceFlag_UnknownRef_FallsBackToFullScan(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		fname := filepath.Join(root, "f"+itoa(i)+".txt")
		if err := os.WriteFile(fname, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(filepath.Base(fname)); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Commit("c"+itoa(i)+"\n", &git.CommitOptions{
			Author: &object.Signature{Name: "T", Email: "t@e", When: time.Date(2026, 5, 1, 12, i, 0, 0, time.UTC)},
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Unknown ref: the scanner currently swallows resolve errors and walks
	// the full history. This test pins that behavior so a future change
	// (e.g. erroring on bad --since) shows up in CI rather than silently
	// changing the contract.
	cfg := &testCfg{depth: 100, since: "v999.999.999-nope"}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	res, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.CommitsScanned != 3 {
		t.Errorf("unknown --since should fall back to full walk; scanned %d, want 3", res.CommitsScanned)
	}
}
