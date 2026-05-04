package fixer

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

type Summary struct {
	FilesChanged      int
	LinesRemoved      int
	GitignoreAdded    int
	IndexEntriesFixed int
	Unfixable         int
}

func (s Summary) Empty() bool {
	return s.FilesChanged == 0 && s.LinesRemoved == 0 && s.GitignoreAdded == 0 && s.IndexEntriesFixed == 0
}

func Apply(root string, fs []findings.Finding, allRules map[string]rules.Rule) (Summary, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Summary{}, err
	}

	var summary Summary
	contentTargets := map[string]map[int][]rules.Rule{}
	ignorePatterns := map[string]struct{}{}
	untrackPaths := map[string]struct{}{}

	for _, f := range fs {
		if f.Baselined || f.Location.Kind != findings.LocFile {
			summary.Unfixable++
			continue
		}
		r, ok := allRules[f.RuleID]
		if !ok {
			summary.Unfixable++
			continue
		}

		switch {
		case r.Kind == rules.KindContent && r.AutoFix.RemoveLine && f.Location.Line > 0 && f.Location.Path != "":
			byLine := contentTargets[f.Location.Path]
			if byLine == nil {
				byLine = map[int][]rules.Rule{}
				contentTargets[f.Location.Path] = byLine
			}
			byLine[f.Location.Line] = append(byLine[f.Location.Line], r)
		case r.Kind == rules.KindPath && len(r.AutoFix.GitignorePatterns) > 0 && f.Location.Path != "":
			for _, p := range r.AutoFix.GitignorePatterns {
				ignorePatterns[p] = struct{}{}
			}
			untrackPaths[f.Location.Path] = struct{}{}
		default:
			summary.Unfixable++
		}
	}

	changedContent, removed, err := removeContentLines(absRoot, contentTargets)
	if err != nil {
		return summary, err
	}
	summary.FilesChanged += changedContent
	summary.LinesRemoved += removed

	added, err := appendGitignore(absRoot, ignorePatterns)
	if err != nil {
		return summary, err
	}
	if added > 0 {
		summary.FilesChanged++
		summary.GitignoreAdded = added
	}

	fixed, err := untrack(absRoot, untrackPaths)
	if err != nil {
		return summary, err
	}
	summary.IndexEntriesFixed = fixed

	return summary, nil
}

func removeContentLines(root string, targets map[string]map[int][]rules.Rule) (int, int, error) {
	var changedFiles, removedLines int
	for rel, byLine := range targets {
		if !safeRel(rel) {
			return changedFiles, removedLines, fmt.Errorf("unsafe path %q", rel)
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			return changedFiles, removedLines, err
		}
		lines := splitLines(data)
		var out [][]byte
		changed := false
		for i, line := range lines {
			lineNo := i + 1
			if shouldRemoveLine(line, byLine[lineNo]) {
				changed = true
				removedLines++
				continue
			}
			out = append(out, line)
		}
		if changed {
			if err := os.WriteFile(path, bytes.Join(out, nil), 0o644); err != nil {
				return changedFiles, removedLines, err
			}
			changedFiles++
		}
	}
	return changedFiles, removedLines, nil
}

func shouldRemoveLine(line []byte, rs []rules.Rule) bool {
	if len(rs) == 0 {
		return false
	}
	line = bytes.TrimRight(line, "\r\n")
	for _, r := range rs {
		for _, p := range r.ContentPatterns {
			re, err := regexp.Compile(p)
			if err == nil && re.Match(line) {
				return true
			}
		}
	}
	return false
}

func splitLines(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	raw := bytes.SplitAfter(data, []byte{'\n'})
	if len(raw[len(raw)-1]) == 0 {
		raw = raw[:len(raw)-1]
	}
	return raw
}

func appendGitignore(root string, patterns map[string]struct{}) (int, error) {
	if len(patterns) == 0 {
		return 0, nil
	}
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	existing := map[string]struct{}{}
	for _, line := range strings.Split(string(data), "\n") {
		existing[strings.TrimSpace(strings.TrimRight(line, "\r"))] = struct{}{}
	}

	missing := make([]string, 0, len(patterns))
	for p := range patterns {
		if _, ok := existing[p]; !ok {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	if len(missing) == 0 {
		return 0, nil
	}

	var buf bytes.Buffer
	buf.Write(data)
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		buf.WriteByte('\n')
	}
	for _, p := range missing {
		buf.WriteString(p)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return 0, err
	}
	return len(missing), nil
}

func untrack(root string, paths map[string]struct{}) (int, error) {
	if len(paths) == 0 {
		return 0, nil
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return 0, nil
	}
	ordered := make([]string, 0, len(paths))
	for p := range paths {
		if !safeRel(p) {
			return 0, fmt.Errorf("unsafe path %q", p)
		}
		ordered = append(ordered, filepath.FromSlash(p))
	}
	sort.Strings(ordered)

	fixed := 0
	for _, p := range ordered {
		check := exec.Command("git", "-C", root, "ls-files", "--error-unmatch", "--", p)
		if err := check.Run(); err != nil {
			continue
		}
		cmd := exec.Command("git", "-C", root, "rm", "--cached", "--ignore-unmatch", "--", p)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fixed, fmt.Errorf("git rm --cached %s: %w: %s", p, err, strings.TrimSpace(string(out)))
		}
		fixed++
	}
	return fixed, nil
}

func safeRel(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	return clean != "." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && clean != ".."
}
