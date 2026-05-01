package engine_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/JadenRazo/llm-lint/internal/config"
	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/rules"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

func TestEngine_E2E_DirtyFixture(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"CLAUDE.md":               "context\n",
		".cursorrules":            "rule\n",
		".claude/settings.json":   "{}\n",
		".aider.conf.yml":         "model: x\n",
		".windsurfrules":          "x\n",
		"src/handler.py":          "def f():\n    # I'm sorry, but I can't help.\n    pass\n",
		"src/legit.go":            "package main\n",
		"vendor/foo/.cursorrules": "vendored, ignored\n",
		".llmlint.yaml":           "version: 1\nignore:\n  - vendor/**\n",
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("marker.txt"); err != nil {
		t.Fatal(err)
	}
	_, err = wt.Commit("feat: add marker\n\nCo-authored-by: Claude <noreply@anthropic.com>\n", &git.CommitOptions{
		Author: &object.Signature{Name: "Tester", Email: "t@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(".llmlint.yaml", root)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(rules.DefaultRegistry(), cfg)
	res, err := eng.Run(root)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]int{}
	for _, f := range res.Findings {
		got[f.RuleID]++
	}
	wantAtLeast := map[string]int{
		"LLM001": 1,
		"LLM002": 1,
		"LLM003": 1,
		"LLM006": 1,
		"LLM008": 1,
		"LLM011": 1,
		"LLM013": 1,
	}
	for id, n := range wantAtLeast {
		if got[id] < n {
			t.Errorf("rule %s: got %d findings, want at least %d (all=%v)", id, got[id], n, got)
		}
	}
	if got["LLM006"] > 1 {
		t.Errorf("LLM006 should match only the non-vendored .cursorrules; got %d (vendor/** ignore not applied)", got["LLM006"])
	}

	if !engine.ExceedsThreshold(res, "error") {
		t.Errorf("expected ExceedsThreshold(error)=true; findings=%v", got)
	}
	if engine.ExceedsThreshold(res, "none") {
		t.Errorf("ExceedsThreshold(none) must always be false")
	}
}

func TestEngine_E2E_CleanRepo_NoFindings(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("docs: init\n", &git.CommitOptions{
		Author: &object.Signature{Name: "Eve", Email: "eve@example.com", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(".llmlint.yaml", root)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(rules.DefaultRegistry(), cfg)
	res, err := eng.Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 0 {
		ids := make([]string, 0, len(res.Findings))
		for _, f := range res.Findings {
			ids = append(ids, f.RuleID)
		}
		sort.Strings(ids)
		t.Errorf("clean repo should have zero findings; got %v", ids)
	}
}
