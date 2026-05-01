package findings_test

import (
	"testing"

	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

func TestFromMatch_FilePath(t *testing.T) {
	t.Parallel()
	m := rules.Match{
		Rule: rules.Rule{
			ID:          "LLM001",
			Title:       "CLAUDE.md committed",
			Severity:    rules.SevError,
			Category:    rules.CatClaude,
			Description: "desc",
			Remediation: "rem",
		},
		Path:    "CLAUDE.md",
		Line:    7,
		Snippet: "context",
	}
	f := findings.FromMatch(m)
	if f.RuleID != "LLM001" {
		t.Errorf("RuleID: got %s want LLM001", f.RuleID)
	}
	if f.Location.Kind != findings.LocFile {
		t.Errorf("Kind: got %s want %s", f.Location.Kind, findings.LocFile)
	}
	if f.Location.Path != "CLAUDE.md" || f.Location.Line != 7 || f.Location.Snippet != "context" {
		t.Errorf("location not copied: %+v", f.Location)
	}
	if f.Location.CommitSHA != "" {
		t.Errorf("commit fields must be empty for file findings; got SHA=%q", f.Location.CommitSHA)
	}
}

func TestFromMatch_Commit(t *testing.T) {
	t.Parallel()
	m := rules.Match{
		Rule:      rules.Rule{ID: "LLM003", Severity: rules.SevError, Category: rules.CatClaude},
		CommitSHA: "abc1234",
		CommitMsg: "feat: x",
		Author:    "Alice <a@example.com>",
		Snippet:   "Co-authored-by: Claude <noreply@anthropic.com>",
	}
	f := findings.FromMatch(m)
	if f.Location.Kind != findings.LocCommit {
		t.Errorf("Kind: got %s want %s", f.Location.Kind, findings.LocCommit)
	}
	if f.Location.CommitSHA != "abc1234" || f.Location.CommitMsg != "feat: x" {
		t.Errorf("commit metadata not copied: %+v", f.Location)
	}
	if f.Location.Path != "" {
		t.Errorf("Path must be empty for commit findings; got %q", f.Location.Path)
	}
}

func TestSort_StableOrdering(t *testing.T) {
	t.Parallel()
	in := []findings.Finding{
		{RuleID: "LLM005", Severity: rules.SevWarning, Location: findings.Location{Kind: findings.LocFile, Path: "a"}},
		{RuleID: "LLM001", Severity: rules.SevError, Location: findings.Location{Kind: findings.LocFile, Path: "z"}},
		{RuleID: "LLM001", Severity: rules.SevError, Location: findings.Location{Kind: findings.LocFile, Path: "a", Line: 5}},
		{RuleID: "LLM001", Severity: rules.SevError, Location: findings.Location{Kind: findings.LocFile, Path: "a", Line: 1}},
		{RuleID: "LLM013", Severity: rules.SevInfo, Location: findings.Location{Kind: findings.LocFile, Path: "x"}},
		{RuleID: "LLM001", Severity: rules.SevError, Location: findings.Location{Kind: findings.LocCommit, CommitSHA: "abc"}},
	}
	findings.Sort(in)

	// Expected: severity desc (error > warning > info), then rule ID asc,
	// then location-kind asc (commit < file alphabetically), then path asc, then line asc.
	want := []struct {
		ID   string
		Sev  rules.Severity
		Path string
		Line int
		SHA  string
	}{
		{"LLM001", rules.SevError, "", 0, "abc"},
		{"LLM001", rules.SevError, "a", 1, ""},
		{"LLM001", rules.SevError, "a", 5, ""},
		{"LLM001", rules.SevError, "z", 0, ""},
		{"LLM005", rules.SevWarning, "a", 0, ""},
		{"LLM013", rules.SevInfo, "x", 0, ""},
	}
	if len(in) != len(want) {
		t.Fatalf("length mismatch: got %d want %d", len(in), len(want))
	}
	for i, w := range want {
		got := in[i]
		if got.RuleID != w.ID || got.Severity != w.Sev ||
			got.Location.Path != w.Path || got.Location.Line != w.Line ||
			got.Location.CommitSHA != w.SHA {
			t.Errorf("position %d: got {%s %s %s:%d sha=%s} want {%s %s %s:%d sha=%s}",
				i,
				got.RuleID, got.Severity, got.Location.Path, got.Location.Line, got.Location.CommitSHA,
				w.ID, w.Sev, w.Path, w.Line, w.SHA)
		}
	}
}

func TestSummarize_Counts(t *testing.T) {
	t.Parallel()
	in := []findings.Finding{
		{Severity: rules.SevError},
		{Severity: rules.SevError},
		{Severity: rules.SevWarning},
		{Severity: rules.SevInfo},
		{Severity: rules.SevInfo},
		{Severity: rules.SevInfo},
	}
	s := findings.Summarize(in)
	if s.Error != 2 || s.Warning != 1 || s.Info != 3 {
		t.Errorf("counts wrong: %+v", s)
	}
}

func TestSummarize_Empty(t *testing.T) {
	t.Parallel()
	s := findings.Summarize(nil)
	if s.Error != 0 || s.Warning != 0 || s.Info != 0 {
		t.Errorf("empty summary should be zero: %+v", s)
	}
}
