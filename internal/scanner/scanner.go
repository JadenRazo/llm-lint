package scanner

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/saracen/walker"

	"github.com/JadenRazo/llm-lint/internal/progress"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

const (
	maxFileSize     = 5 * 1024 * 1024
	maxContentSize  = 2 * 1024 * 1024
	binarySniffSize = 8 * 1024
)

type Config interface {
	IsRuleEnabled(id string) bool
	EffectiveSeverity(id string, def rules.Severity) rules.Severity
	IsIgnored(relPath string) bool
	// IsIgnoredDir reports whether a whole directory can be pruned
	// (e.g. "vendor/**" prunes the vendor directory itself).
	IsIgnoredDir(relDir string) bool
}

type Stats struct {
	FilesWalked  int64
	FilesScanned int64
	BytesRead    int64
	// FilesUnreadable counts files that matched the scan filters but could
	// not be read. Surfaced in reports so an unreadable file is
	// distinguishable from a clean one.
	FilesUnreadable int64
}

type Scanner struct {
	cfg            Config
	pathRules      []rules.Rule
	contentRules   []rules.Rule
	contentRegexes [][]*regexp.Regexp
}

func New(cfg Config, allRules map[string]rules.Rule) (*Scanner, error) {
	s := &Scanner{cfg: cfg}
	for _, r := range allRules {
		if !cfg.IsRuleEnabled(r.ID) {
			continue
		}
		switch r.Kind {
		case rules.KindPath:
			s.pathRules = append(s.pathRules, r)
		case rules.KindContent:
			compiled := make([]*regexp.Regexp, 0, len(r.ContentPatterns))
			for _, p := range r.ContentPatterns {
				re, err := regexp.Compile(p)
				if err != nil {
					return nil, fmt.Errorf("rule %s: invalid regex %q: %w", r.ID, p, err)
				}
				compiled = append(compiled, re)
			}
			s.contentRules = append(s.contentRules, r)
			s.contentRegexes = append(s.contentRegexes, compiled)
		}
	}
	return s, nil
}

func (s *Scanner) Scan(root string) ([]rules.Match, Stats, error) {
	return s.ScanWithProgress(context.Background(), root, nil)
}

func (s *Scanner) ScanWithProgress(ctx context.Context, root string, prog *progress.Reporter) ([]rules.Match, Stats, error) {
	var stats Stats
	var (
		mu      sync.Mutex
		matches []rules.Match
	)

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, stats, err
	}

	gi := loadGitignoreMatcher(absRoot)

	walkFn := func(path string, info os.FileInfo) error {
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(absRoot, path)
			relSlash := filepath.ToSlash(rel)
			if rel != "." && s.cfg.IsIgnoredDir(relSlash) {
				return filepath.SkipDir
			}
			if gi.match(relSlash, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > maxFileSize {
			return nil
		}

		atomic.AddInt64(&stats.FilesWalked, 1)

		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			atomic.AddInt64(&stats.FilesUnreadable, 1)
			return nil
		}
		rel = filepath.ToSlash(rel)
		if s.cfg.IsIgnored(rel) {
			return nil
		}
		if gi.match(rel, false) {
			return nil
		}

		atomic.AddInt64(&stats.FilesScanned, 1)
		prog.IncFile()

		var fileMatches []rules.Match
		for _, r := range s.pathRules {
			if matchesAnyGlob(rel, r.PathGlobs) {
				fileMatches = append(fileMatches, rules.Match{
					Rule: applySeverity(r, s.cfg),
					Path: rel,
				})
			}
		}

		if len(s.contentRules) > 0 && info.Size() <= maxContentSize {
			contentMatches, n, readErr := s.scanContent(path, rel, info.Size())
			if readErr != nil {
				atomic.AddInt64(&stats.FilesUnreadable, 1)
			}
			fileMatches = append(fileMatches, contentMatches...)
			atomic.AddInt64(&stats.BytesRead, n)
		}

		if len(fileMatches) > 0 {
			mu.Lock()
			matches = append(matches, fileMatches...)
			mu.Unlock()
		}
		return nil
	}

	errCb := walker.WithErrorCallback(func(_ string, err error) error {
		if errors_isPermission(err) || errors_isNotExist(err) {
			return nil
		}
		return err
	})

	if err := walker.WalkWithContext(ctx, absRoot, walkFn, errCb); err != nil {
		return nil, stats, err
	}
	return matches, stats, nil
}

func (s *Scanner) scanContent(absPath, rel string, size int64) ([]rules.Match, int64, error) {
	if size == 0 {
		return nil, 0, nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, 0, err
	}
	return s.applyContentRules(rel, data), int64(len(data)), nil
}

// applyContentRules runs the compiled content-rule regex set against an
// in-memory buffer. Used by both filesystem (scanContent) and index
// (ScanIndex) iterators. Returns nil for binary content.
func (s *Scanner) applyContentRules(rel string, data []byte) []rules.Match {
	if len(data) == 0 {
		return nil
	}
	if isBinary(data) {
		return nil
	}

	var out []rules.Match
	lines := bytes.Split(data, []byte{'\n'})
	for i, r := range s.contentRules {
		regs := s.contentRegexes[i]
		for lineIdx, line := range lines {
			for _, re := range regs {
				if loc := re.FindIndex(line); loc != nil {
					out = append(out, rules.Match{
						Rule:    applySeverity(r, s.cfg),
						Path:    rel,
						Line:    lineIdx + 1,
						Snippet: trim(string(line)),
					})
					break
				}
			}
		}
	}
	return out
}

func matchesAnyGlob(rel string, globs []string) bool {
	for _, g := range globs {
		// Match (not PathMatch): rel is slash-normalized, and PathMatch
		// splits on the OS separator, which breaks Windows. Built-in rule
		// globs are compile-time constants, so the error path is dead.
		ok, err := doublestar.Match(g, rel)
		if err == nil && ok {
			return true
		}
	}
	return false
}

func isBinary(data []byte) bool {
	n := len(data)
	if n > binarySniffSize {
		n = binarySniffSize
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

func trim(s string) string {
	s = strings.TrimRight(s, "\r")
	if len(s) > 200 {
		return s[:197] + "..."
	}
	return s
}

func applySeverity(r rules.Rule, cfg Config) rules.Rule {
	r.Severity = cfg.EffectiveSeverity(r.ID, r.Severity)
	return r
}

func errors_isPermission(err error) bool { return os.IsPermission(err) }
func errors_isNotExist(err error) bool   { return os.IsNotExist(err) }

var _ = fs.ModeSymlink
