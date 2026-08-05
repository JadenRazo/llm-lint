package report

import (
	"fmt"
	"io"

	"github.com/owenrumney/go-sarif/v2/sarif"

	"github.com/JadenRazo/llm-lint/internal/baseline"
	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/rules"
	"github.com/JadenRazo/llm-lint/internal/textutil"
)

type SARIFReporter struct {
	w      io.Writer
	closer io.Closer
	opts   Options
}

// rulesHelpURI is where every rule's helpUri points; individual rule docs
// are anchors under the README's Rules section.
const rulesHelpURI = "https://github.com/JadenRazo/llm-lint#rules"

// automationID identifies llm-lint runs in SARIF consumers that group
// results by runAutomationDetails (e.g. GitHub code scanning categories).
const automationID = "llm-lint/scan"

// fingerprintKey names the partialFingerprints entry. Versioned so the
// hashing scheme can evolve without colliding with old uploads.
const fingerprintKey = "llmLint/v1"

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
	run.WithAutomationDetails(sarif.NewRunAutomationDetails().WithID(automationID))

	added := map[string]bool{}
	for _, f := range res.Findings {
		if !added[f.RuleID] {
			added[f.RuleID] = true
			run.AddRule(f.RuleID).
				WithName(f.Title).
				WithShortDescription(sarif.NewMultiformatMessageString(f.Title)).
				WithFullDescription(sarif.NewMultiformatMessageString(f.Description)).
				WithHelp(sarif.NewMultiformatMessageString(f.Remediation)).
				WithHelpURI(rulesHelpURI).
				WithDefaultConfiguration(sarif.NewReportingConfiguration().WithLevel(sarifLevel(f.Severity))).
				WithProperties(sarif.Properties{
					"category": string(f.Category),
					// GitHub code scanning reads security-severity to
					// bucket alerts (critical/high/medium/low).
					"security-severity": securitySeverity(f.Severity),
				})
		}
	}

	for _, f := range res.Findings {
		baselineState := "new"
		if f.Baselined {
			baselineState = "unchanged"
		}
		result := sarif.NewRuleResult(f.RuleID).
			WithLevel(sarifLevel(f.Severity)).
			WithMessage(sarif.NewTextMessage(f.Title)).
			WithBaselineState(baselineState)

		// Stable identity across runs: same hash the baseline uses, so a
		// finding tracked in code scanning survives line shifts and re-runs.
		if fp := baseline.Fingerprint(f); fp != "" {
			result.WithPartialFingerPrints(map[string]interface{}{fingerprintKey: fp})
		}

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
					WithArtifactLocation(sarif.NewSimpleArtifactLocation(".git/COMMIT_" + textutil.ShortSHA(f.Location.CommitSHA))),
			)
			result.AddLocation(loc)
		}
		run.AddResult(result)
	}

	report.AddRun(run)
	return report.PrettyWrite(r.w)
}

// securitySeverity maps llm-lint severities onto the 0-10 scale GitHub
// code scanning expects in rule properties (as a string).
func securitySeverity(s rules.Severity) string {
	switch s {
	case rules.SevError:
		return "8.0"
	case rules.SevWarning:
		return "5.0"
	case rules.SevInfo:
		return "3.0"
	}
	return "0.0"
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
