package report_test

import (
	"bytes"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/report"
	"github.com/JadenRazo/llm-lint/internal/rules"
	"github.com/JadenRazo/llm-lint/internal/testutil"

	// Register built-in rules so baseline.Fingerprint (used for SARIF
	// partialFingerprints) resolves the fixture's rule IDs.
	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

// fixedResult is the canonical fixture for reporter golden comparisons.
// Spans every Severity (error/warning/info), every Location.Kind (file/commit),
// and every rule Kind that produces a finding (path, content, git-trailer, git-message).
// Sorted by findings.Sort so the same input is identical across runs.
func fixedResult() *engine.Result {
	res := &engine.Result{
		FilesScanned:   42,
		FilesWalked:    57,
		BytesRead:      12345,
		CommitsScanned: 7,
		DurationMS:     0, // pinned for determinism
		Findings: []findings.Finding{
			{
				RuleID:      "LLM001",
				Title:       "CLAUDE.md committed",
				Severity:    rules.SevError,
				Category:    rules.CatClaude,
				Description: "CLAUDE.md is read by Claude Code as repo-specific context.",
				Remediation: "Add CLAUDE.md to .gitignore and untrack with git rm --cached.",
				Location:    findings.Location{Kind: findings.LocFile, Path: "CLAUDE.md"},
			},
			{
				RuleID:      "LLM003",
				Title:       "Co-authored-by: Claude trailer",
				Severity:    rules.SevError,
				Category:    rules.CatClaude,
				Description: "Commit trailer attributing co-authorship to Claude.",
				Remediation: "Set includeCoAuthoredBy: false in ~/.claude/settings.json.",
				Location: findings.Location{
					Kind:      findings.LocCommit,
					CommitSHA: "abc1234567890abcdef1234567890abcdef12345",
					CommitMsg: "feat: example",
					Author:    "Alice <alice@example.com>",
					Snippet:   "Co-authored-by: Claude <noreply@anthropic.com>",
				},
			},
			{
				RuleID:      "LLM004",
				Title:       "Claude attribution in commit message",
				Severity:    rules.SevWarning,
				Category:    rules.CatClaude,
				Description: "Commit message body advertises Claude generation.",
				Remediation: "Set includeCoAuthoredBy: false to stop the footer.",
				Location: findings.Location{
					Kind:      findings.LocCommit,
					CommitSHA: "def4567890abcdef1234567890abcdef12345678",
					CommitMsg: "fix: example",
					Author:    "Bob <bob@example.com>",
				},
			},
			{
				RuleID:      "LLM013",
				Title:       "AI refusal/boilerplate string in source",
				Severity:    rules.SevInfo,
				Category:    rules.CatGeneric,
				Description: "Telltale LLM refusal text in source.",
				Remediation: "Remove the offending text or add to .llmlint.yaml ignore.",
				Location: findings.Location{
					Kind:    findings.LocFile,
					Path:    "src/handler.py",
					Line:    12,
					Snippet: "    # placeholder refusal note",
				},
			},
		},
		Summary: findings.Summary{Error: 2, Warning: 1, Info: 1},
	}
	findings.Sort(res.Findings)
	return res
}

// normalizeSARIF strips fields that go-sarif emits with run-time variation
// (run-level UUID/GUID) so the golden compare is stable across CI runs.
// We keep enough of the document that schema validation in TestSARIF_Validates
// still proves correctness; this only blanks the non-deterministic bits.
func normalizeSARIF(b []byte) []byte {
	// automationDetails is deterministic now that the reporter pins its id,
	// so only the run-level GUIDs need blanking.
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`"guid":\s*"[0-9a-fA-F-]+"`),
		regexp.MustCompile(`"runGuid":\s*"[0-9a-fA-F-]+"`),
	}
	out := b
	for _, re := range patterns {
		out = re.ReplaceAll(out, []byte(`"_normalized_": ""`))
	}
	return out
}

func goldenPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	testutil.CompareGolden(t, goldenPath(t, name), got)
}

func TestJSONReporter_Golden(t *testing.T) {
	res := fixedResult()
	var buf bytes.Buffer
	rep := &report.JSONReporter{}
	report.SetJSONReporterFields(rep, &buf, "0.1.0-test")
	if err := rep.Write(res); err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "result.json.golden", buf.Bytes())

	// Determinism: run twice, second run must match the golden too.
	var buf2 bytes.Buffer
	rep2 := &report.JSONReporter{}
	report.SetJSONReporterFields(rep2, &buf2, "0.1.0-test")
	res2 := fixedResult()
	if err := rep2.Write(res2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), buf2.Bytes()) {
		t.Errorf("JSON output is not deterministic; two runs differ:\n--- run1 ---\n%s\n--- run2 ---\n%s", buf.Bytes(), buf2.Bytes())
	}
}

func TestSARIFReporter_Golden(t *testing.T) {
	res := fixedResult()
	var buf bytes.Buffer
	rep := &report.SARIFReporter{}
	report.SetReporterFields(rep, &buf, "0.1.0-test")
	if err := rep.Write(res); err != nil {
		t.Fatal(err)
	}
	got := normalizeSARIF(buf.Bytes())
	compareGolden(t, "result.sarif.golden", got)

	// Determinism check on the *normalized* form (we tolerate run-level GUIDs
	// that go-sarif assigns; we don't tolerate field-order or content drift).
	var buf2 bytes.Buffer
	rep2 := &report.SARIFReporter{}
	report.SetReporterFields(rep2, &buf2, "0.1.0-test")
	if err := rep2.Write(fixedResult()); err != nil {
		t.Fatal(err)
	}
	got2 := normalizeSARIF(buf2.Bytes())
	if !bytes.Equal(got, got2) {
		t.Errorf("SARIF output (normalized) is not deterministic across runs:\n--- run1 ---\n%s\n--- run2 ---\n%s", got, got2)
	}
}
