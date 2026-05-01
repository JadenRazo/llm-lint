package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/rules"
	"github.com/JadenRazo/llm-lint/internal/scanner"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

// markGitRepo creates the minimal .git marker that loadGitignoreMatcher
// looks for. Real git internals are not required because gitignore.ReadPatterns
// only reads .gitignore files from the worktree, not the index.
func markGitRepo(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func hasRule(matches []rules.Match, ruleID string) bool {
	for _, m := range matches {
		if m.Rule.ID == ruleID {
			return true
		}
	}
	return false
}

// Case 1: gitignored CLAUDE.md in a git repo must NOT trigger LLM001.
func TestScanner_Gitignore_SkipsClaudeMd(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	markGitRepo(t, root)
	writeFiles(t, root, map[string]string{
		".gitignore": "CLAUDE.md\n",
		"CLAUDE.md":  "context\n",
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
	if hasRule(matches, "LLM001") {
		t.Errorf("LLM001 must not fire on a gitignored CLAUDE.md; matches=%+v", matches)
	}
}

// Case 2: gitignored .claude/ subtree in a git repo must NOT trigger LLM002.
func TestScanner_Gitignore_SkipsClaudeDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	markGitRepo(t, root)
	writeFiles(t, root, map[string]string{
		".gitignore":            ".claude/\n",
		".claude/settings.json": "{}\n",
		".claude/state.txt":     "x\n",
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
	if hasRule(matches, "LLM002") {
		t.Errorf("LLM002 must not fire on a gitignored .claude/ tree; matches=%+v", matches)
	}
}

// Case 3: in a git repo with no .gitignore, current behavior is preserved
// (path-rules still fire on present-but-untracked files).
func TestScanner_GitRepoNoGitignore_StillFlags(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	markGitRepo(t, root)
	writeFiles(t, root, map[string]string{
		"CLAUDE.md": "context\n",
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
	if !hasRule(matches, "LLM001") {
		t.Errorf("LLM001 must fire when .gitignore is absent; matches=%+v", matches)
	}
}

// Case 4: a non-git directory (no .git/) keeps the original filesystem-only
// behavior — gitignore is never consulted, regardless of any .gitignore file
// that might happen to exist.
func TestScanner_NonGitDir_GitignoreNotConsulted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// no markGitRepo — this is a plain directory.
	writeFiles(t, root, map[string]string{
		".gitignore": "CLAUDE.md\n",
		"CLAUDE.md":  "context\n",
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
	if !hasRule(matches, "LLM001") {
		t.Errorf("LLM001 must fire in a non-git dir even with a .gitignore present; matches=%+v", matches)
	}
}

// Case 5: nested .gitignore is honored within its scope without leaking out.
// A subtree-local CLAUDE.md is skipped; an unrelated CLAUDE.md elsewhere fires.
func TestScanner_Gitignore_NestedScope(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	markGitRepo(t, root)
	writeFiles(t, root, map[string]string{
		"vendor/.gitignore": "CLAUDE.md\n",
		"vendor/CLAUDE.md":  "vendored notes\n",
		"src/CLAUDE.md":     "real notes\n",
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
	var got []string
	for _, m := range matches {
		if m.Rule.ID == "LLM001" {
			got = append(got, m.Path)
		}
	}
	if len(got) != 1 || got[0] != "src/CLAUDE.md" {
		t.Errorf("expected LLM001 only on src/CLAUDE.md (vendor/ scope-local gitignore); got %v", got)
	}
}
