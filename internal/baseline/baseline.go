// Package baseline manages .llmlint-baseline.yaml — a snapshot of accepted
// findings that subsequent `llm-lint scan` runs treat as suppressed-from-CI
// without losing visibility (they still appear in human output, marked
// "(baselined)"; SARIF emits baselineState=unchanged).
//
// The point: a team can adopt llm-lint on a repo with historical artifacts,
// snapshot the current state, gate CI on new findings, and shrink the
// baseline over time toward zero.
//
// Fingerprint stability is the load-bearing property. See fingerprint.go
// for the per-finding-kind composition rules.
package baseline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/JadenRazo/llm-lint/internal/findings"
)

const (
	// BaselineSchemaVersion is the on-disk format version. Bump when the
	// fingerprint algorithm or entry shape changes incompatibly.
	BaselineSchemaVersion = 1

	// DefaultPath is where Save writes and Load reads when no path is given.
	DefaultPath = ".llmlint-baseline.yaml"

	locFile    = "file"
	locTrailer = "git_trailer"
	locMessage = "git_message"
)

// Document is the on-disk shape.
type Document struct {
	Version     int     `json:"version"`
	GeneratedAt string  `json:"generated_at"`
	GeneratedBy string  `json:"generated_by"`
	Total       int     `json:"total"`
	Entries     []Entry `json:"entries"`
}

// Entry is one snapshotted finding. Fingerprint is the source of truth for
// matching; the other fields are for human review of diffs.
type Entry struct {
	Rule        string `json:"rule"`
	Fingerprint string `json:"fingerprint"`
	Location    string `json:"location"` // file | git_trailer | git_message
	Path        string `json:"path,omitempty"`
	Line        int    `json:"line,omitempty"`
	SHA         string `json:"sha,omitempty"`
	Snippet     string `json:"snippet,omitempty"`
	Message     string `json:"message,omitempty"`
}

// New returns an empty Document at the current schema version.
func New(generatedBy string) *Document {
	return &Document{
		Version:     BaselineSchemaVersion,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		GeneratedBy: generatedBy,
		Entries:     nil,
	}
}

// Load reads and validates a baseline file. Returns (nil, nil) when path
// does not exist — callers decide if missing is an error.
func Load(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc Document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.Version > BaselineSchemaVersion {
		return nil, fmt.Errorf("baseline %s written by newer llm-lint (version=%d, supported=%d); please upgrade",
			path, doc.Version, BaselineSchemaVersion)
	}
	if doc.Version == 0 {
		return nil, fmt.Errorf("baseline %s has invalid version 0", path)
	}
	return &doc, nil
}

// Save writes the document atomically: tmp file in same dir, fsync, rename.
// Sorts entries (rule asc, then fingerprint asc) for diff-friendly output.
// Validates round-trip via Load before rename, so a serialization bug
// can't ship a broken baseline.
func Save(doc *Document, path string) error {
	if doc == nil {
		return errors.New("baseline: nil document")
	}
	doc.Total = len(doc.Entries)
	if doc.Version == 0 {
		doc.Version = BaselineSchemaVersion
	}
	sort.SliceStable(doc.Entries, func(i, j int) bool {
		if doc.Entries[i].Rule != doc.Entries[j].Rule {
			return doc.Entries[i].Rule < doc.Entries[j].Rule
		}
		return doc.Entries[i].Fingerprint < doc.Entries[j].Fingerprint
	})

	body, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal baseline: %w", err)
	}
	if !strings.HasSuffix(string(body), "\n") {
		body = append(body, '\n')
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".llmlint-baseline.*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op after successful rename

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}

	// Round-trip validation before rename.
	if _, err := Load(tmpPath); err != nil {
		return fmt.Errorf("post-write validation: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// ApplyStats is the result of Apply.
type ApplyStats struct {
	// Matched is how many findings were marked Baselined=true.
	Matched int
	// Stale lists baseline entries that did not match any current finding.
	Stale []Entry
	// Total is the entry count in the baseline doc.
	Total int
}

// Apply mutates findings in place: for every finding whose Fingerprint
// appears in doc.Entries, set Baselined=true. Returns stats including
// stale baseline entries (entries with no matching finding).
func Apply(fs []findings.Finding, doc *Document) ApplyStats {
	stats := ApplyStats{}
	if doc == nil {
		return stats
	}
	stats.Total = len(doc.Entries)

	want := make(map[string]Entry, len(doc.Entries))
	for _, e := range doc.Entries {
		want[e.Fingerprint] = e
	}

	matched := make(map[string]bool, len(doc.Entries))
	for i := range fs {
		fp := Fingerprint(fs[i])
		if fp == "" {
			continue
		}
		if _, ok := want[fp]; ok {
			fs[i].Baselined = true
			matched[fp] = true
			stats.Matched++
		}
	}

	for fp, e := range want {
		if !matched[fp] {
			stats.Stale = append(stats.Stale, e)
		}
	}
	sort.Slice(stats.Stale, func(i, j int) bool {
		if stats.Stale[i].Rule != stats.Stale[j].Rule {
			return stats.Stale[i].Rule < stats.Stale[j].Rule
		}
		return stats.Stale[i].Fingerprint < stats.Stale[j].Fingerprint
	})
	return stats
}

// BuildOptions controls Entry construction.
type BuildOptions struct {
	// IncludeSnippets, when false, omits the Snippet/Message context
	// fields. Useful for teams that don't want match text in VCS.
	IncludeSnippets bool
}

// BuildEntries converts findings to baseline entries. Findings whose rule
// is unknown to the fingerprint algorithm are skipped silently; callers
// that care can compare Total to the input length.
func BuildEntries(fs []findings.Finding, opts BuildOptions) []Entry {
	out := make([]Entry, 0, len(fs))
	for _, f := range fs {
		fp := Fingerprint(f)
		if fp == "" {
			continue
		}
		e := Entry{
			Rule:        f.RuleID,
			Fingerprint: fp,
			Location:    locationLabel(f),
		}
		switch e.Location {
		case locFile:
			e.Path = f.Location.Path
			if f.Location.Line > 0 {
				e.Line = f.Location.Line
			}
			if opts.IncludeSnippets {
				e.Snippet = f.Location.Snippet
			}
		case locTrailer, locMessage:
			e.SHA = f.Location.CommitSHA
			if opts.IncludeSnippets {
				e.Message = f.Location.Snippet
			}
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		return out[i].Fingerprint < out[j].Fingerprint
	})
	return out
}

// ResolvePath joins a configured baseline path against a repo root if
// the path is relative.
func ResolvePath(path, root string) string {
	if path == "" {
		path = DefaultPath
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}
