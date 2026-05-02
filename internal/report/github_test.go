package report_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/report"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

func TestEscapeAnnotationMessage_Table(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"plain ascii", "plain ascii"},
		{"100%", "100%25"},
		{"a%b", "a%25b"},
		{"%%", "%25%25"},
		{"a\nb", "a%0Ab"},
		{"a\rb", "a%0Db"},
		{"a\r\nb", "a%0D%0Ab"},
		{"90%\n50%", "90%25%0A50%25"},
		// Comma and colon pass through in message context.
		{",a:b", ",a:b"},
		// UTF-8 passes through byte-for-byte.
		{"日本", "日本"},
	}
	for _, c := range cases {
		got := report.EscapeAnnotationMessage(c.in)
		if got != c.want {
			t.Errorf("EscapeAnnotationMessage(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestEscapeAnnotationProperty_Table(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"plain", "plain"},
		{"a, b: c%", "a%2C b%3A c%25"},
		{"src/foo,bar.go", "src/foo%2Cbar.go"},
		{"line\nbreak", "line%0Abreak"},
	}
	for _, c := range cases {
		got := report.EscapeAnnotationProperty(c.in)
		if got != c.want {
			t.Errorf("EscapeAnnotationProperty(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestAnnotationLevel(t *testing.T) {
	cases := []struct {
		sev  rules.Severity
		want string
	}{
		{rules.SevError, "error"},
		{rules.SevWarning, "warning"},
		{rules.SevInfo, "notice"},
		{"", "notice"},
		{"unknown", "notice"},
	}
	for _, c := range cases {
		got := report.AnnotationLevel(c.sev)
		if got != c.want {
			t.Errorf("AnnotationLevel(%q): got %q want %q", c.sev, got, c.want)
		}
	}
}

func TestAutoDetectFormat(t *testing.T) {
	cases := []struct {
		name           string
		envVal         string
		formatChanged  bool
		current        string
		want           string
	}{
		{"in-actions, no flag, defaults to github", "true", false, "human", "github"},
		{"in-actions, user passed flag, respects user", "true", true, "json", "json"},
		{"not-in-actions, no flag, current wins", "", false, "human", "human"},
		{"not-in-actions, user passed flag, respects", "", true, "sarif", "sarif"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := func(k string) string {
				if k == "GITHUB_ACTIONS" {
					return c.envVal
				}
				return ""
			}
			got := report.AutoDetectFormat(env, c.formatChanged, c.current)
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestGitHubReporter_AnnotationsOnlyForFileFindings(t *testing.T) {
	res := fixedResult()
	var buf bytes.Buffer
	rep, err := report.New("github", report.Options{Output: "-", Version: "0.1.0-test"})
	if err != nil {
		t.Fatal(err)
	}
	report.SetGitHubReporterFields(rep.(*report.GitHubReporter), &buf, func(string) string { return "" })
	if err := rep.Write(res); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// File-located findings (LLM001 path, LLM013 content) → annotations.
	if !strings.Contains(out, "::error file=CLAUDE.md,") {
		t.Errorf("expected LLM001 annotation; got:\n%s", out)
	}
	if !strings.Contains(out, "::notice file=src/handler.py,") {
		t.Errorf("expected LLM013 notice annotation; got:\n%s", out)
	}
	// Commit-located findings (LLM003, LLM004) MUST NOT produce annotations.
	if strings.Contains(out, "LLM003") {
		t.Errorf("commit findings should not produce annotations; got LLM003 in output:\n%s", out)
	}
	if strings.Contains(out, "LLM004") {
		t.Errorf("commit findings should not produce annotations; got LLM004 in output:\n%s", out)
	}
}

func TestGitHubReporter_AnnotationLineFormat(t *testing.T) {
	f := findings.Finding{
		RuleID:      "LLM001",
		Title:       "CLAUDE.md committed",
		Severity:    rules.SevError,
		Description: "CLAUDE.md is read by Claude Code.",
		Remediation: "Delete it.",
		Location:    findings.Location{Kind: findings.LocFile, Path: "CLAUDE.md", Line: 0},
	}
	res := stubResult(f)
	var buf bytes.Buffer
	rep, err := report.New("github", report.Options{Output: "-", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	report.SetGitHubReporterFields(rep.(*report.GitHubReporter), &buf, func(string) string { return "" })
	if err := rep.Write(res); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	wantPrefix := "::error file=CLAUDE.md,line=1,col=1,title=LLM001%3A CLAUDE.md committed::"
	if !strings.Contains(got, wantPrefix) {
		t.Errorf("annotation prefix mismatch.\ngot:  %s\nwant: %s...", got, wantPrefix)
	}
}

func TestGitHubReporter_NoFindingsSummary(t *testing.T) {
	res := stubResult()

	tmpFile := t.TempDir() + "/summary.md"
	env := func(k string) string {
		switch k {
		case "GITHUB_STEP_SUMMARY":
			return tmpFile
		}
		return ""
	}

	var buf bytes.Buffer
	rep, err := report.New("github", report.Options{Output: "-", Version: "0.1.0-test"})
	if err != nil {
		t.Fatal(err)
	}
	report.SetGitHubReporterFields(rep.(*report.GitHubReporter), &buf, env)
	if err := rep.Write(res); err != nil {
		t.Fatal(err)
	}

	body, err := readFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "## llm-lint findings") {
		t.Errorf("summary missing header; got:\n%s", body)
	}
	if !strings.Contains(body, "No findings.") {
		t.Errorf("clean run should say 'No findings.'; got:\n%s", body)
	}
}

func TestGitHubReporter_StepSummaryAppendsNotTruncates(t *testing.T) {
	res := fixedResult()
	tmpFile := t.TempDir() + "/summary.md"
	if err := writeFile(tmpFile, "PRE-EXISTING\n"); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "GITHUB_STEP_SUMMARY" {
			return tmpFile
		}
		return ""
	}
	var buf bytes.Buffer
	rep, _ := report.New("github", report.Options{Output: "-", Version: "0.1.0-test"})
	report.SetGitHubReporterFields(rep.(*report.GitHubReporter), &buf, env)
	if err := rep.Write(res); err != nil {
		t.Fatal(err)
	}
	body, _ := readFile(tmpFile)
	if !strings.HasPrefix(body, "PRE-EXISTING\n") {
		t.Errorf("pre-existing content must be preserved; got:\n%s", body)
	}
	if !strings.Contains(body, "## llm-lint findings") {
		t.Errorf("expected llm-lint summary appended; got:\n%s", body)
	}
}

func TestGitHubReporter_TruncatesLongMessage(t *testing.T) {
	long := strings.Repeat("X", 8000)
	f := findings.Finding{
		RuleID:      "LLM001",
		Title:       "X",
		Severity:    rules.SevError,
		Description: long,
		Location:    findings.Location{Kind: findings.LocFile, Path: "f.go", Line: 1},
	}
	var buf bytes.Buffer
	rep, _ := report.New("github", report.Options{Output: "-", Version: "test"})
	report.SetGitHubReporterFields(rep.(*report.GitHubReporter), &buf, func(string) string { return "" })
	if err := rep.Write(stubResult(f)); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if len(out) > 4500 {
		t.Errorf("annotation line should be truncated to ~4096; got %d bytes", len(out))
	}
	if !strings.Contains(out, "[truncated]") {
		t.Errorf("expected [truncated] suffix; got:\n%s", out[:200])
	}
}
