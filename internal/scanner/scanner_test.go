package scanner_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/rules"
	"github.com/JadenRazo/llm-lint/internal/scanner"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

type testCfg struct {
	disabled map[string]bool
	ignore   []string
}

func (c *testCfg) IsRuleEnabled(id string) bool {
	return !c.disabled[id]
}

func (c *testCfg) EffectiveSeverity(id string, def rules.Severity) rules.Severity {
	return def
}

func (c *testCfg) IsIgnored(rel string) bool {
	for _, p := range c.ignore {
		if p == rel {
			return true
		}
	}
	return false
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func ruleIDs(matches []rules.Match) []string {
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.Rule.ID+":"+m.Path)
	}
	sort.Strings(ids)
	return ids
}

func TestScanner_PathRules(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"CLAUDE.md":               "context\n",
		".cursorrules":            "rule\n",
		".claude/settings.json":   "{}\n",
		"src/legit.go":            "package main\n",
		".aider.conf.yml":         "model: x\n",
		"vendor/foo/.cursorrules": "ignored\n",
	})

	cfg := &testCfg{ignore: []string{"vendor/foo/.cursorrules"}}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, _, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}

	got := ruleIDs(matches)
	want := []string{
		"LLM001:CLAUDE.md",
		"LLM002:.claude/settings.json",
		"LLM006:.cursorrules",
		"LLM008:.aider.conf.yml",
	}
	sort.Strings(want)
	if !equal(got, want) {
		t.Errorf("matches mismatch:\n got=%v\nwant=%v", got, want)
	}
}

func TestScanner_ContentRule_FiresOnRefusalString(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/handler.py": "def f():\n    # I'm sorry, but I can't do that\n    pass\n",
		"src/clean.py":   "def f(): pass\n",
	})

	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, _, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}

	var hit *rules.Match
	for i, m := range matches {
		if m.Rule.ID == "LLM013" {
			hit = &matches[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected LLM013 hit; got matches=%v", ruleIDs(matches))
	}
	if hit.Path != "src/handler.py" {
		t.Errorf("expected path src/handler.py; got %s", hit.Path)
	}
	if hit.Line != 2 {
		t.Errorf("expected line 2; got %d", hit.Line)
	}
}

func TestScanner_BinaryFilesSkipped(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/data.bin": "\x00\x00\x00binary garbage with the phrase as an AI language model embedded\x00",
	})
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
		if m.Rule.ID == "LLM013" {
			t.Errorf("binary file should not trigger content rules; got %v", m)
		}
	}
}

func TestScanner_DotGitDirSkipped(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		".git/CLAUDE.md": "should be ignored\n",
	})
	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, _, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf(".git/ contents must be skipped; got %v", ruleIDs(matches))
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
