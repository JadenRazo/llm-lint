package report

import (
	"encoding/json"
	"io"

	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
)

type JSONReporter struct {
	w      io.Writer
	closer io.Closer
	opts   Options
}

type jsonOutput struct {
	Tool     toolInfo           `json:"tool"`
	Scanned  scannedInfo        `json:"scanned"`
	Findings []findings.Finding `json:"findings"`
	Summary  findings.Summary   `json:"summary"`
}

type toolInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type scannedInfo struct {
	Files          int64 `json:"files"`
	Commits        int   `json:"commits"`
	DurationMS     int64 `json:"duration_ms"`
	GitShallow     bool  `json:"git_shallow,omitempty"`
	GitSkipped     bool  `json:"git_skipped,omitempty"`
	GitSkippedNote string `json:"git_skipped_note,omitempty"`
}

func (r *JSONReporter) Write(res *engine.Result) error {
	if r.closer != nil {
		defer r.closer.Close()
	}
	out := jsonOutput{
		Tool: toolInfo{Name: "llm-lint", Version: r.opts.Version},
		Scanned: scannedInfo{
			Files:          res.FilesScanned,
			Commits:        res.CommitsScanned,
			DurationMS:     res.DurationMS,
			GitShallow:     res.GitShallow,
			GitSkipped:     res.GitSkipped,
			GitSkippedNote: res.GitSkippedNote,
		},
		Findings: res.Findings,
		Summary:  res.Summary,
	}
	if out.Findings == nil {
		out.Findings = []findings.Finding{}
	}
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
