package report_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xeipuuv/gojsonschema"

	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/report"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

func TestSARIF_ValidatesAgainstSchema(t *testing.T) {
	res := &engine.Result{
		FilesScanned:   42,
		CommitsScanned: 7,
		DurationMS:     12,
		Findings: []findings.Finding{
			{
				RuleID:      "LLM001",
				Title:       "CLAUDE.md committed",
				Severity:    rules.SevError,
				Category:    rules.CatClaude,
				Description: "Hidden context file",
				Remediation: "Add to .gitignore",
				Location:    findings.Location{Kind: findings.LocFile, Path: "CLAUDE.md", Line: 1},
			},
			{
				RuleID:      "LLM003",
				Title:       "Co-authored-by Claude",
				Severity:    rules.SevError,
				Category:    rules.CatClaude,
				Description: "AI trailer",
				Remediation: "Set includeCoAuthoredBy: false",
				Location: findings.Location{
					Kind:      findings.LocCommit,
					CommitSHA: "abc1234567890abcdef1234567890abcdef12345",
					CommitMsg: "feat: x",
					Author:    "Alice <a@example.com>",
				},
			},
		},
		Summary: findings.Summary{Error: 2},
	}

	var buf bytes.Buffer
	rep := &report.SARIFReporter{}
	report.SetReporterFields(rep, &buf, "0.1.0")
	if err := rep.Write(res); err != nil {
		t.Fatalf("write: %v", err)
	}

	var doc any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput=%s", err, buf.String())
	}

	schemaPath := schemaPath(t)
	schemaLoader := gojsonschema.NewReferenceLoader("file://" + schemaPath)
	docLoader := gojsonschema.NewBytesLoader(buf.Bytes())
	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		t.Fatalf("schema load: %v", err)
	}
	if !result.Valid() {
		var errs []string
		for _, e := range result.Errors() {
			errs = append(errs, e.String())
		}
		t.Errorf("SARIF output failed schema validation:\n%s\n--- output ---\n%s",
			strings.Join(errs, "\n"), buf.String())
	}
}

func schemaPath(t *testing.T) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	return filepath.Join(dir, "testdata", "sarif-2.1.0.json")
}
