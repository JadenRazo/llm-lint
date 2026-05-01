package report_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/report"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

func TestJSONReporter_StructureAndFields(t *testing.T) {
	res := &engine.Result{
		FilesScanned:   3,
		CommitsScanned: 1,
		DurationMS:     5,
		Findings: []findings.Finding{
			{
				RuleID:   "LLM001",
				Title:    "CLAUDE.md committed",
				Severity: rules.SevError,
				Category: rules.CatClaude,
				Location: findings.Location{Kind: findings.LocFile, Path: "CLAUDE.md"},
			},
		},
		Summary: findings.Summary{Error: 1},
	}

	var buf bytes.Buffer
	rep := &report.JSONReporter{}
	report.SetJSONReporterFields(rep, &buf, "0.1.0")
	if err := rep.Write(res); err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Tool struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"tool"`
		Scanned struct {
			Files int64 `json:"files"`
		} `json:"scanned"`
		Findings []map[string]any `json:"findings"`
		Summary  struct {
			Error int `json:"error"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v\noutput=%s", err, buf.String())
	}
	if doc.Tool.Name != "llm-lint" {
		t.Errorf("tool name: got %q want llm-lint", doc.Tool.Name)
	}
	if doc.Tool.Version != "0.1.0" {
		t.Errorf("version: got %q want 0.1.0", doc.Tool.Version)
	}
	if doc.Scanned.Files != 3 {
		t.Errorf("files scanned: got %d want 3", doc.Scanned.Files)
	}
	if len(doc.Findings) != 1 {
		t.Fatalf("findings: got %d want 1", len(doc.Findings))
	}
	if doc.Summary.Error != 1 {
		t.Errorf("summary.error: got %d want 1", doc.Summary.Error)
	}
	if !strings.Contains(buf.String(), "LLM001") {
		t.Errorf("output should include rule id; got:\n%s", buf.String())
	}
}

func TestJSONReporter_EmptyFindings(t *testing.T) {
	res := &engine.Result{}
	var buf bytes.Buffer
	rep := &report.JSONReporter{}
	report.SetJSONReporterFields(rep, &buf, "0.1.0")
	if err := rep.Write(res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"findings": []`) {
		t.Errorf("empty findings should emit []; got:\n%s", buf.String())
	}
}

func TestNew_UnknownFormat(t *testing.T) {
	_, err := report.New("yaml", report.Options{Output: "-"})
	if err == nil {
		t.Error("expected error for unknown format")
	}
}

func TestNew_HumanFormat_Default(t *testing.T) {
	rep, err := report.New("", report.Options{Output: "-", NoColor: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil {
		t.Error("expected reporter")
	}
}

func TestHumanReporter_NoFindings(t *testing.T) {
	res := &engine.Result{FilesScanned: 0}
	var buf bytes.Buffer
	rep := &report.HumanReporter{}
	report.SetHumanReporterFields(rep, &buf, "0.1.0", true)
	if err := rep.Write(res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no findings") {
		t.Errorf("expected 'no findings' in output; got:\n%s", buf.String())
	}
}

func TestHumanReporter_WithFindings(t *testing.T) {
	res := &engine.Result{
		FilesScanned: 5,
		Findings: []findings.Finding{
			{
				RuleID:      "LLM001",
				Title:       "CLAUDE.md committed",
				Severity:    rules.SevError,
				Category:    rules.CatClaude,
				Remediation: "Add to .gitignore.",
				Location:    findings.Location{Kind: findings.LocFile, Path: "CLAUDE.md"},
			},
			{
				RuleID:   "LLM003",
				Title:    "Co-authored-by: Claude trailer",
				Severity: rules.SevError,
				Category: rules.CatClaude,
				Location: findings.Location{
					Kind:      findings.LocCommit,
					CommitSHA: "abcdef1234567890",
					CommitMsg: "feat: x",
					Author:    "Bob <b@example.com>",
				},
			},
		},
		Summary: findings.Summary{Error: 2},
	}
	var buf bytes.Buffer
	rep := &report.HumanReporter{}
	report.SetHumanReporterFields(rep, &buf, "0.1.0", true)
	if err := rep.Write(res); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"LLM001", "LLM003", "CLAUDE.md", "abcdef1", "2 errors"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, out)
		}
	}
}
