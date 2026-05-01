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

type testCfg struct {
	disabled map[string]bool
	since    string
	depth    int
}

func (c *testCfg) IsRuleEnabled(id string) bool                              { return !c.disabled[id] }
func (c *testCfg) EffectiveSeverity(_ string, def rules.Severity) rules.Severity { return def }
func (c *testCfg) HistoryDepth() int                                         { return c.depth }
func (c *testCfg) Since() string                                             { return c.since }

func makeRepo(t *testing.T, commits []commit) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range commits {
		fname := filepath.Join(dir, "f"+itoa(i)+".txt")
		if err := os.WriteFile(fname, []byte(c.fileContent), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(filepath.Base(fname)); err != nil {
			t.Fatal(err)
		}
		_, err := wt.Commit(c.msg, &git.CommitOptions{
			Author: &object.Signature{
				Name:  c.author,
				Email: c.email,
				When:  time.Date(2026, 5, 1, 12, i, 0, 0, time.UTC),
			},
			AllowEmptyCommits: false,
		})
		if err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
	}
	return dir
}

type commit struct {
	msg         string
	fileContent string
	author      string
	email       string
}

func TestGitScan_DetectsClaudeTrailer(t *testing.T) {
	root := makeRepo(t, []commit{
		{
			msg: "feat: clean commit\n\nNothing here.\n",
			fileContent: "x", author: "Alice", email: "alice@corp.example",
		},
		{
			msg: "feat: dirty commit\n\nLooks nice.\n\nCo-authored-by: Claude <noreply@anthropic.com>\n",
			fileContent: "y", author: "Bob", email: "bob@corp.example",
		},
	})

	cfg := &testCfg{depth: 100}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	res, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.CommitsScanned != 2 {
		t.Errorf("commits scanned: got %d want 2", res.CommitsScanned)
	}
	hits := 0
	for _, m := range res.Matches {
		if m.Rule.ID == "LLM003" {
			hits++
			if !contains(m.Snippet, "Claude") {
				t.Errorf("expected snippet to mention Claude, got %q", m.Snippet)
			}
		}
	}
	if hits != 1 {
		t.Errorf("expected 1 LLM003 hit; got %d", hits)
	}
}

func TestGitScan_HumanNamedClaude_NotFlagged(t *testing.T) {
	// A real contributor named Claude with a personal (non-Anthropic) email
	// must not trigger LLM003. Locks in the regex tightening that removed
	// the over-broad `claude\s*<` pattern.
	root := makeRepo(t, []commit{
		{
			msg:         "feat: thing\n\nCo-authored-by: Claude Dupont <claude@example.com>\n",
			fileContent: "h", author: "Alice", email: "alice@corp.example",
		},
	})
	cfg := &testCfg{depth: 100}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	res, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range res.Matches {
		if m.Rule.ID == "LLM003" {
			t.Errorf("LLM003 must not flag a real contributor named Claude with a non-Anthropic email; snippet=%q", m.Snippet)
		}
	}
}

func TestGitScan_DetectsClaudeMessageFooter(t *testing.T) {
	root := makeRepo(t, []commit{
		{
			msg: "feat: thing\n\n🤖 Generated with [Claude Code](https://claude.com/code)\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n",
			fileContent: "z", author: "Carol", email: "carol@corp.example",
		},
	})
	cfg := &testCfg{depth: 100}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	res, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, m := range res.Matches {
		got[m.Rule.ID]++
	}
	if got["LLM003"] == 0 {
		t.Errorf("expected LLM003; matches=%v", got)
	}
	if got["LLM004"] == 0 {
		t.Errorf("expected LLM004 (message rule); matches=%v", got)
	}
}

func TestGitScan_DetectsCopilotTrailer(t *testing.T) {
	root := makeRepo(t, []commit{
		{
			msg: "feat: do thing\n\nCo-authored-by: GitHub Copilot <copilot@github.com>\n",
			fileContent: "p", author: "Dan", email: "dan@corp.example",
		},
	})
	cfg := &testCfg{depth: 100}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	res, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	for _, m := range res.Matches {
		if m.Rule.ID == "LLM012" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("expected LLM012 (Copilot trailer)")
	}
}

func TestGitScan_CleanRepo_NoFindings(t *testing.T) {
	root := makeRepo(t, []commit{
		{msg: "feat: clean\n", fileContent: "a", author: "Eve", email: "eve@corp.example"},
		{msg: "fix: also clean\n\nSigned-off-by: Eve <eve@corp.example>\n", fileContent: "b", author: "Eve", email: "eve@corp.example"},
	})
	cfg := &testCfg{depth: 100}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	res, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 0 {
		t.Errorf("expected no findings; got %v", res.Matches)
	}
}

func TestGitScan_NotARepo_ReturnsErr(t *testing.T) {
	dir := t.TempDir()
	cfg := &testCfg{depth: 100}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	_, err := s.Scan(dir)
	if err == nil {
		t.Errorf("expected error scanning non-git dir")
	}
}

func TestGitScan_DepthLimit(t *testing.T) {
	commits := make([]commit, 0, 10)
	for i := 0; i < 10; i++ {
		commits = append(commits, commit{
			msg:         "commit " + itoa(i) + "\n",
			fileContent: itoa(i),
			author:      "X", email: "x@example.com",
		})
	}
	root := makeRepo(t, commits)
	cfg := &testCfg{depth: 3}
	s := gitscan.New(rules.DefaultRegistry(), cfg)
	res, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.CommitsScanned != 3 {
		t.Errorf("depth=3 should scan 3 commits; got %d", res.CommitsScanned)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := ""
	if i < 0 {
		neg = "-"
		i = -i
	}
	out := ""
	for i > 0 {
		out = string(rune('0'+i%10)) + out
		i /= 10
	}
	return neg + out
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
