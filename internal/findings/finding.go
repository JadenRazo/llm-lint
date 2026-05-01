package findings

import (
	"sort"

	"github.com/JadenRazo/llm-lint/internal/rules"
)

type LocationKind string

const (
	LocFile   LocationKind = "file"
	LocCommit LocationKind = "commit"
)

type Location struct {
	Kind      LocationKind `json:"kind"`
	Path      string       `json:"path,omitempty"`
	Line      int          `json:"line,omitempty"`
	Snippet   string       `json:"snippet,omitempty"`
	CommitSHA string       `json:"commit_sha,omitempty"`
	CommitMsg string       `json:"commit_msg,omitempty"`
	Author    string       `json:"author,omitempty"`
}

type Finding struct {
	RuleID      string         `json:"rule_id"`
	Title       string         `json:"title"`
	Severity    rules.Severity `json:"severity"`
	Category    rules.Category `json:"category"`
	Description string         `json:"description"`
	Remediation string         `json:"remediation"`
	Location    Location       `json:"location"`
}

func FromMatch(m rules.Match) Finding {
	loc := Location{}
	if m.CommitSHA != "" {
		loc.Kind = LocCommit
		loc.CommitSHA = m.CommitSHA
		loc.CommitMsg = m.CommitMsg
		loc.Author = m.Author
	} else {
		loc.Kind = LocFile
		loc.Path = m.Path
		loc.Line = m.Line
		loc.Snippet = m.Snippet
	}
	return Finding{
		RuleID:      m.Rule.ID,
		Title:       m.Rule.Title,
		Severity:    m.Rule.Severity,
		Category:    m.Rule.Category,
		Description: m.Rule.Description,
		Remediation: m.Rule.Remediation,
		Location:    loc,
	}
}

func Sort(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		ri, rj := fs[i].Severity.Rank(), fs[j].Severity.Rank()
		if ri != rj {
			return ri > rj
		}
		if fs[i].RuleID != fs[j].RuleID {
			return fs[i].RuleID < fs[j].RuleID
		}
		ki, kj := string(fs[i].Location.Kind), string(fs[j].Location.Kind)
		if ki != kj {
			return ki < kj
		}
		if fs[i].Location.Path != fs[j].Location.Path {
			return fs[i].Location.Path < fs[j].Location.Path
		}
		if fs[i].Location.Line != fs[j].Location.Line {
			return fs[i].Location.Line < fs[j].Location.Line
		}
		return fs[i].Location.CommitSHA < fs[j].Location.CommitSHA
	})
}

type Summary struct {
	Error   int `json:"error"`
	Warning int `json:"warning"`
	Info    int `json:"info"`
}

func Summarize(fs []Finding) Summary {
	var s Summary
	for _, f := range fs {
		switch f.Severity {
		case rules.SevError:
			s.Error++
		case rules.SevWarning:
			s.Warning++
		case rules.SevInfo:
			s.Info++
		}
	}
	return s
}
