package engine_test

import (
	"testing"

	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

func makeFinding(sev rules.Severity) findings.Finding {
	return findings.Finding{
		RuleID:   "LLM999",
		Severity: sev,
		Location: findings.Location{Kind: findings.LocFile, Path: "x"},
	}
}

func TestExceedsThreshold_Matrix(t *testing.T) {
	t.Parallel()

	none := []findings.Finding{}
	infoOnly := []findings.Finding{makeFinding(rules.SevInfo)}
	warnOnly := []findings.Finding{makeFinding(rules.SevWarning)}
	errOnly := []findings.Finding{makeFinding(rules.SevError)}
	mixed := []findings.Finding{makeFinding(rules.SevInfo), makeFinding(rules.SevWarning)}

	cases := []struct {
		name    string
		findset []findings.Finding
		failOn  string
		want    bool
	}{
		{"empty/error", none, "error", false},
		{"empty/warning", none, "warning", false},
		{"empty/info", none, "info", false},
		{"empty/none", none, "none", false},
		{"empty/empty-string", none, "", false},

		{"info/error", infoOnly, "error", false},
		{"info/warning", infoOnly, "warning", false},
		{"info/info", infoOnly, "info", true},
		{"info/none", infoOnly, "none", false},
		{"info/empty-string", infoOnly, "", false},

		{"warn/error", warnOnly, "error", false},
		{"warn/warning", warnOnly, "warning", true},
		{"warn/info", warnOnly, "info", true},
		{"warn/none", warnOnly, "none", false},

		{"err/error", errOnly, "error", true},
		{"err/warning", errOnly, "warning", true},
		{"err/info", errOnly, "info", true},
		{"err/none", errOnly, "none", false},

		{"mixed/error", mixed, "error", false},
		{"mixed/warning", mixed, "warning", true},
		{"mixed/info", mixed, "info", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := &engine.Result{Findings: tc.findset}
			if got := engine.ExceedsThreshold(res, tc.failOn); got != tc.want {
				t.Errorf("ExceedsThreshold(failOn=%q) = %v, want %v", tc.failOn, got, tc.want)
			}
		})
	}
}

func TestExceedsThreshold_UnknownLevelTreatedAsZero(t *testing.T) {
	t.Parallel()
	// rules.Severity("garbage").Rank() returns 0, so any non-zero severity
	// finding (info=1) trips it. Document that current behavior is "unknown
	// strings exceed everything" so a regression that flips this surfaces.
	res := &engine.Result{Findings: []findings.Finding{makeFinding(rules.SevInfo)}}
	if !engine.ExceedsThreshold(res, "garbage") {
		t.Error("unknown severity string should rank as 0 and be exceeded by any finding")
	}
}
