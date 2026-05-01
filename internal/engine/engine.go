package engine

import (
	"fmt"
	"time"

	"github.com/JadenRazo/llm-lint/internal/config"
	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/gitscan"
	"github.com/JadenRazo/llm-lint/internal/progress"
	"github.com/JadenRazo/llm-lint/internal/rules"
	"github.com/JadenRazo/llm-lint/internal/scanner"
)

type Result struct {
	Findings        []findings.Finding `json:"findings"`
	Summary         findings.Summary   `json:"summary"`
	FilesScanned    int64              `json:"files_scanned"`
	CommitsScanned  int                `json:"commits_scanned"`
	DurationMS      int64              `json:"duration_ms"`
	GitShallow      bool               `json:"git_shallow,omitempty"`
	GitSkipped      bool               `json:"git_skipped,omitempty"`
	GitSkippedNote  string             `json:"git_skipped_note,omitempty"`
}

type Engine struct {
	allRules map[string]rules.Rule
	cfg      *config.Config
	prog     *progress.Reporter
}

func New(allRules map[string]rules.Rule, cfg *config.Config) *Engine {
	return &Engine{allRules: allRules, cfg: cfg}
}

// WithProgress wires a progress reporter that ticks during scan.
// Pass nil (or skip) to disable.
func (e *Engine) WithProgress(p *progress.Reporter) *Engine {
	e.prog = p
	return e
}

func (e *Engine) Run(root string) (*Result, error) {
	start := time.Now()
	res := &Result{}

	if e.prog != nil {
		defer e.prog.Done()
	}

	if e.cfg.FilesystemEnabled() {
		s, err := scanner.New(e.cfg, e.allRules)
		if err != nil {
			return nil, fmt.Errorf("scanner init: %w", err)
		}
		if e.prog != nil {
			e.prog.Phase("files")
		}
		matches, stats, err := s.ScanWithProgress(root, e.prog)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		for _, m := range matches {
			res.Findings = append(res.Findings, findings.FromMatch(m))
		}
		res.FilesScanned = stats.FilesScanned
	}

	if e.cfg.GitEnabled() {
		gs := gitscan.New(e.allRules, e.cfg)
		if e.prog != nil {
			e.prog.Phase("git")
		}
		gres, err := gs.ScanWithProgress(root, e.prog)
		if err != nil {
			res.GitSkipped = true
			res.GitSkippedNote = err.Error()
		} else {
			res.GitShallow = gres.Shallow
			res.CommitsScanned = gres.CommitsScanned
			for _, m := range gres.Matches {
				res.Findings = append(res.Findings, findings.FromMatch(m))
			}
		}
	}

	findings.Sort(res.Findings)
	res.Summary = findings.Summarize(res.Findings)
	res.DurationMS = time.Since(start).Milliseconds()
	return res, nil
}

func ExceedsThreshold(r *Result, failOn string) bool {
	threshold := rules.Severity(failOn).Rank()
	if failOn == "" || failOn == "none" {
		return false
	}
	for _, f := range r.Findings {
		if f.Severity.Rank() >= threshold {
			return true
		}
	}
	return false
}
