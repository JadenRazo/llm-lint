package engine_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"sigs.k8s.io/yaml"

	"github.com/JadenRazo/llm-lint/internal/baseline"
	"github.com/JadenRazo/llm-lint/internal/config"
	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
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

func TestEngine_E2E_StagedOnly_SkipsTrailerRules(t *testing.T) {
	// Staged-only mode reads the git index for path+content rules and
	// skips trailer/message rules entirely (no commit yet at pre-commit
	// time). A commit with a Claude trailer must NOT fire LLM003 here.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("x"), 0o644); err != nil {
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
	if _, err := wt.Add("marker.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("feat: marker\n\nCo-authored-by: Claude <noreply@anthropic.com>\n", &git.CommitOptions{
		Author: &object.Signature{Name: "Tester", Email: "t@example.com", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	// Stage CLAUDE.md (path rule LLM001) but do not commit it.
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("CLAUDE.md"); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(".llmlint.yaml", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ApplyCLIOverrides(config.CLIOverrides{StagedOnly: true}); err != nil {
		t.Fatal(err)
	}

	res, err := engine.New(rules.DefaultRegistry(), cfg).Run(root)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]int{}
	for _, f := range res.Findings {
		got[f.RuleID]++
	}
	if got["LLM001"] != 1 {
		t.Errorf("staged CLAUDE.md should fire LLM001 once; got %d (all=%v)", got["LLM001"], got)
	}
	if got["LLM003"] != 0 {
		t.Errorf("staged-only must skip trailer rules; got LLM003 count %d (all=%v)", got["LLM003"], got)
	}
}

func TestEngine_E2E_Baseline_NewFindingFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".llmlint.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(".llmlint.yaml", root)
	if err != nil {
		t.Fatal(err)
	}

	res, err := engine.New(rules.DefaultRegistry(), cfg).Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("expected 1 finding (CLAUDE.md); got %d", len(res.Findings))
	}

	// Snapshot the finding into a baseline file.
	doc := &baselineDoc{
		Version:     1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		GeneratedBy: "test",
		Total:       1,
		Entries: []baselineEntry{
			{Rule: res.Findings[0].RuleID, Fingerprint: fingerprintFor(res.Findings[0]), Location: "file", Path: "CLAUDE.md"},
		},
	}
	writeBaseline(t, filepath.Join(root, ".llmlint-baseline.yaml"), doc)

	// Re-scan: the existing finding should be baselined, threshold not exceeded.
	cfg2, err := config.Load(".llmlint.yaml", root)
	if err != nil {
		t.Fatal(err)
	}
	res2, err := engine.New(rules.DefaultRegistry(), cfg2).Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.BaselineLoaded {
		t.Errorf("baseline file should be loaded")
	}
	if res2.BaselinedCount != 1 {
		t.Errorf("baselined: got %d want 1", res2.BaselinedCount)
	}
	if engine.ExceedsThreshold(res2, "error") {
		t.Errorf("threshold should not be exceeded when only finding is baselined")
	}

	// Add a new (un-baselined) finding: a .cursorrules file.
	if err := os.WriteFile(filepath.Join(root, ".cursorrules"), []byte("rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg3, _ := config.Load(".llmlint.yaml", root)
	res3, err := engine.New(rules.DefaultRegistry(), cfg3).Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.ExceedsThreshold(res3, "error") {
		t.Errorf("new finding (.cursorrules) should fail the threshold even with baseline applied")
	}
}

// baselineDoc / baselineEntry / writeBaseline / fingerprintFor are a
// minimal local copy to avoid an import cycle (engine_test → baseline →
// findings; engine already imports baseline).
type baselineDoc struct {
	Version     int             `json:"version"`
	GeneratedAt string          `json:"generated_at"`
	GeneratedBy string          `json:"generated_by"`
	Total       int             `json:"total"`
	Entries     []baselineEntry `json:"entries"`
}

type baselineEntry struct {
	Rule        string `json:"rule"`
	Fingerprint string `json:"fingerprint"`
	Location    string `json:"location"`
	Path        string `json:"path,omitempty"`
}

func writeBaseline(t *testing.T, path string, doc *baselineDoc) {
	t.Helper()
	body, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fingerprintFor(f findings.Finding) string {
	// Mirrors baseline.Fingerprint for path findings without the import cycle.
	return baseline.Fingerprint(f)
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
