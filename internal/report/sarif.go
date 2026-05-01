package report

import (
	"fmt"
	"io"

	"github.com/owenrumney/go-sarif/v2/sarif"

	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

type SARIFReporter struct {
	w      io.Writer
	closer io.Closer
	opts   Options
}

func (r *SARIFReporter) Write(res *engine.Result) error {
	if r.closer != nil {
		defer r.closer.Close()
	}

	report, err := sarif.New(sarif.Version210)
	if err != nil {
		return fmt.Errorf("sarif new: %w", err)
	}
	run := sarif.NewRunWithInformationURI("llm-lint", "https://github.com/JadenRazo/llm-lint")
	run.Tool.Driver.WithVersion(r.opts.Version)

	added := map[string]bool{}
	for _, f := range res.Findings {
		if !added[f.RuleID] {
			added[f.RuleID] = true
			run.AddRule(f.RuleID).
				WithName(f.Title).
				WithShortDescription(sarif.NewMultiformatMessageString(f.Title)).
				WithFullDescription(sarif.NewMultiformatMessageString(f.Description)).
				WithHelp(sarif.NewMultiformatMessageString(f.Remediation)).
				WithDefaultConfiguration(sarif.NewReportingConfiguration().WithLevel(sarifLevel(f.Severity))).
				WithProperties(sarif.Properties{"category": string(f.Category)})
		}
	}

	for _, f := range res.Findings {
		result := sarif.NewRuleResult(f.RuleID).
			WithLevel(sarifLevel(f.Severity)).
			WithMessage(sarif.NewTextMessage(f.Title))

		if f.Location.Kind == findings.LocFile && f.Location.Path != "" {
			region := sarif.NewSimpleRegion(maxInt(f.Location.Line, 1), maxInt(f.Location.Line, 1))
			loc := sarif.NewLocationWithPhysicalLocation(
				sarif.NewPhysicalLocation().
					WithArtifactLocation(sarif.NewSimpleArtifactLocation(f.Location.Path)).
					WithRegion(region),
			)
			result.AddLocation(loc)
		} else if f.Location.Kind == findings.LocCommit {
			result.Properties = sarif.Properties{
				"commit_sha": f.Location.CommitSHA,
				"commit_msg": f.Location.CommitMsg,
				"author":     f.Location.Author,
			}
			loc := sarif.NewLocationWithPhysicalLocation(
				sarif.NewPhysicalLocation().
					WithArtifactLocation(sarif.NewSimpleArtifactLocation(".git/COMMIT_" + short(f.Location.CommitSHA))),
			)
			result.AddLocation(loc)
		}
		run.AddResult(result)
	}

	report.AddRun(run)
	return report.PrettyWrite(r.w)
}

func sarifLevel(s rules.Severity) string {
	switch s {
	case rules.SevError:
		return "error"
	case rules.SevWarning:
		return "warning"
	case rules.SevInfo:
		return "note"
	}
	return "none"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
