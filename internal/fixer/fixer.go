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

type GitHistoryMode string

const (
	GitHistoryNone    GitHistoryMode = "none"
	GitHistoryLatest  GitHistoryMode = "latest"
	GitHistoryScanned GitHistoryMode = "scanned"
)

type Options struct {
	GitHistoryMode string
	Preview        bool
}

type Summary struct {
	FilesChanged       int
	LinesRemoved       int
	CommitMessages     int
	CommitLinesRemoved int
	GitignoreAdded     int
	IndexEntriesFixed  int
	Unfixable          int
}

func (s Summary) Empty() bool {
	return s.FilesChanged == 0 && s.LinesRemoved == 0 && s.CommitMessages == 0 && s.GitignoreAdded == 0 && s.IndexEntriesFixed == 0
}

func Apply(root string, fs []findings.Finding, allRules map[string]rules.Rule) (Summary, error) {
	return ApplyWithOptions(root, fs, allRules, Options{GitHistoryMode: string(GitHistoryLatest)})
}

func ApplyWithOptions(root string, fs []findings.Finding, allRules map[string]rules.Rule, opts Options) (Summary, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Summary{}, err
	}
	mode := GitHistoryMode(opts.GitHistoryMode)
	if mode == "" {
		mode = GitHistoryLatest
	}
	switch mode {
	case GitHistoryNone, GitHistoryLatest, GitHistoryScanned:
	default:
		return Summary{}, fmt.Errorf("invalid git history fix mode %q", mode)
	}

	var summary Summary
	contentTargets := map[string]map[int][]rules.Rule{}
	commitTargets := map[string][]rules.Rule{}
	ignorePatterns := map[string]struct{}{}
	untrackPaths := map[string]struct{}{}

	for _, f := range fs {
		if f.Baselined {
			summary.Unfixable++
			continue
		}
		r, ok := allRules[f.RuleID]
		if !ok {
			summary.Unfixable++
			continue
		}
		switch {
		case f.Location.Kind == findings.LocFile && r.Kind == rules.KindContent && r.AutoFix.RemoveLine && f.Location.Line > 0 && f.Location.Path != "":
			byLine := contentTargets[f.Location.Path]
			if byLine == nil {
				byLine = map[int][]rules.Rule{}
				contentTargets[f.Location.Path] = byLine
			}
			byLine[f.Location.Line] = append(byLine[f.Location.Line], r)
		case f.Location.Kind == findings.LocFile && r.Kind == rules.KindPath && len(r.AutoFix.GitignorePatterns) > 0 && f.Location.Path != "":
			for _, p := range r.AutoFix.GitignorePatterns {
				ignorePatterns[p] = struct{}{}
			}
			untrackPaths[f.Location.Path] = struct{}{}
		case f.Location.Kind == findings.LocCommit && r.AutoFix.AmendLatestCommit && f.Location.CommitSHA != "":
			commitTargets[f.Location.CommitSHA] = append(commitTargets[f.Location.CommitSHA], r)
		default:
			summary.Unfixable++
		}
	}

	commitMessages, commitLinesRemoved, unfixable, err := fixCommitMessages(absRoot, commitTargets, mode, opts.Preview)
	if err != nil {
		return summary, err
	}
	summary.CommitMessages = commitMessages
	summary.CommitLinesRemoved = commitLinesRemoved
	summary.Unfixable += unfixable

	changedContent, removed, err := removeContentLines(absRoot, contentTargets, opts.Preview)
	if err != nil {
		return summary, err
	}
	summary.FilesChanged += changedContent
	summary.LinesRemoved += removed

	added, err := appendGitignore(absRoot, ignorePatterns, opts.Preview)
	if err != nil {
		return summary, err
	}
	if added > 0 {
		summary.FilesChanged++
		summary.GitignoreAdded = added
	}

	fixed, err := untrack(absRoot, untrackPaths, opts.Preview)
	if err != nil {
		return summary, err
	}
	summary.IndexEntriesFixed = fixed

	return summary, nil
}

func removeContentLines(root string, targets map[string]map[int][]rules.Rule, preview bool) (int, int, error) {
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
			if preview {
				changedFiles++
				continue
			}
			if err := os.WriteFile(path, bytes.Join(out, nil), 0o644); err != nil {
				return changedFiles, removedLines, err
			}
			changedFiles++
		}
	}
	return changedFiles, removedLines, nil
}

func fixCommitMessages(root string, targets map[string][]rules.Rule, mode GitHistoryMode, preview bool) (int, int, int, error) {
	if len(targets) == 0 {
		return 0, 0, 0, nil
	}
	if mode == GitHistoryNone {
		return 0, 0, lenCommitTargets(targets), nil
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return 0, 0, lenCommitTargets(targets), nil
	}
	if mode == GitHistoryScanned {
		return rewriteScannedCommitMessages(root, targets, preview)
	}

	head, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return 0, 0, lenCommitTargets(targets), nil
	}
	head = strings.TrimSpace(head)

	var unfixable int
	var headRules []rules.Rule
	for sha, rs := range targets {
		if sha == head {
			headRules = append(headRules, rs...)
			continue
		}
		unfixable += len(rs)
	}
	if len(headRules) == 0 {
		return 0, 0, unfixable, nil
	}

	msg, err := gitOutput(root, "log", "-1", "--format=%B")
	if err != nil {
		return 0, 0, unfixable, err
	}
	cleaned, removed := cleanCommitMessage(msg, headRules)
	if removed == 0 || cleaned == msg {
		return 0, 0, unfixable + len(headRules), nil
	}
	if strings.TrimSpace(cleaned) == "" {
		return 0, 0, unfixable, fmt.Errorf("refusing to auto-fix HEAD commit message to empty")
	}
	meta, err := loadCommitMeta(root, "HEAD")
	if err != nil {
		return 0, 0, unfixable, err
	}
	if preview {
		return 1, removed, unfixable, nil
	}
	newHead, err := createCommit(root, meta, cleaned, strings.Fields(meta.Parents))
	if err != nil {
		return 0, 0, unfixable, err
	}
	if err := updateHead(root, head, newHead); err != nil {
		return 0, 0, unfixable, err
	}
	return 1, removed, unfixable, nil
}

func rewriteScannedCommitMessages(root string, targets map[string][]rules.Rule, preview bool) (int, int, int, error) {
	oldHead, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return 0, 0, lenCommitTargets(targets), nil
	}
	oldHead = strings.TrimSpace(oldHead)

	revList, err := gitOutput(root, "rev-list", "--reverse", "--topo-order", "HEAD")
	if err != nil {
		return 0, 0, 0, err
	}
	rewrite := map[string]string{}
	seenTargets := map[string]struct{}{}
	changedCommits, removedLines := 0, 0
	unfixable := 0

	for _, sha := range strings.Fields(revList) {
		meta, err := loadCommitMeta(root, sha)
		if err != nil {
			return changedCommits, removedLines, unfixable, err
		}
		msg := meta.Message
		cleaned := msg
		removed := 0
		if rs := targets[sha]; len(rs) > 0 {
			seenTargets[sha] = struct{}{}
			cleaned, removed = cleanCommitMessage(msg, rs)
			if removed > 0 && strings.TrimSpace(cleaned) == "" {
				return changedCommits, removedLines, unfixable, fmt.Errorf("refusing to auto-fix commit %s message to empty", shortSHA(sha))
			}
			if removed == 0 {
				unfixable += len(rs)
			}
		}

		parents := strings.Fields(meta.Parents)
		newParents := make([]string, 0, len(parents))
		parentChanged := false
		for _, p := range parents {
			np := rewrite[p]
			if np == "" {
				np = p
			}
			if np != p {
				parentChanged = true
			}
			newParents = append(newParents, np)
		}

		if removed == 0 && !parentChanged {
			rewrite[sha] = sha
			continue
		}

		newSHA := sha
		if !preview {
			var err error
			newSHA, err = createCommit(root, meta, cleaned, newParents)
			if err != nil {
				return changedCommits, removedLines, unfixable, err
			}
		}
		rewrite[sha] = newSHA
		if removed > 0 {
			changedCommits++
			removedLines += removed
		}
	}

	newHead := rewrite[oldHead]
	if newHead == "" {
		return changedCommits, removedLines, lenCommitTargets(targets), nil
	}
	if newHead != oldHead && !preview {
		if err := updateHead(root, oldHead, newHead); err != nil {
			return changedCommits, removedLines, unfixable, err
		}
	}
	for sha, rs := range targets {
		if _, ok := seenTargets[sha]; !ok {
			unfixable += len(rs)
		}
	}
	return changedCommits, removedLines, unfixable, nil
}

func cleanCommitMessage(msg string, rs []rules.Rule) (string, int) {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	lines := strings.Split(msg, "\n")
	out := make([]string, 0, len(lines))
	removed := 0
	for _, line := range lines {
		if shouldRemoveCommitLine(line, rs) {
			removed++
			continue
		}
		out = append(out, line)
	}
	cleaned := strings.TrimRight(strings.Join(out, "\n"), "\n")
	if cleaned != "" {
		cleaned += "\n"
	}
	return cleaned, removed
}

func shouldRemoveCommitLine(line string, rs []rules.Rule) bool {
	line = strings.TrimRight(line, "\r")
	for _, r := range rs {
		patterns := r.MessagePatterns
		if r.Kind == rules.KindGitTrailer {
			patterns = r.TrailerPatterns
		}
		for _, p := range patterns {
			re, err := regexp.Compile(p)
			if err == nil && re.MatchString(line) {
				return true
			}
		}
	}
	return false
}

type commitMeta struct {
	Tree           string
	Parents        string
	AuthorName     string
	AuthorEmail    string
	AuthorDate     string
	CommitterName  string
	CommitterEmail string
	CommitterDate  string
	Message        string
}

func loadCommitMeta(root, sha string) (commitMeta, error) {
	metaOut, err := gitOutput(root, "show", "-s", "--format=%T%x00%P%x00%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI", sha)
	if err != nil {
		return commitMeta{}, err
	}
	parts := strings.Split(strings.TrimRight(metaOut, "\n"), "\x00")
	if len(parts) != 8 {
		return commitMeta{}, fmt.Errorf("unexpected commit metadata for %s", shortSHA(sha))
	}
	msg, err := gitOutput(root, "log", "-1", "--format=%B", sha)
	if err != nil {
		return commitMeta{}, err
	}
	return commitMeta{
		Tree:           strings.TrimSpace(parts[0]),
		Parents:        parts[1],
		AuthorName:     parts[2],
		AuthorEmail:    parts[3],
		AuthorDate:     parts[4],
		CommitterName:  parts[5],
		CommitterEmail: parts[6],
		CommitterDate:  parts[7],
		Message:        msg,
	}, nil
}

func createCommit(root string, meta commitMeta, msg string, parents []string) (string, error) {
	tmp, err := os.CreateTemp(root, ".llm-lint-commit-msg-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.WriteString(msg); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	args := gitArgs(root, "commit-tree", meta.Tree)
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	args = append(args, "-F", tmpPath)

	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+meta.AuthorName,
		"GIT_AUTHOR_EMAIL="+meta.AuthorEmail,
		"GIT_AUTHOR_DATE="+meta.AuthorDate,
		"GIT_COMMITTER_NAME="+meta.CommitterName,
		"GIT_COMMITTER_EMAIL="+meta.CommitterEmail,
		"GIT_COMMITTER_DATE="+meta.CommitterDate,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git commit-tree: %w: %s", err, strings.TrimSpace(string(out)))
	}
	newSHA := strings.TrimSpace(string(out))
	if newSHA == "" {
		return "", fmt.Errorf("git commit-tree returned empty commit id")
	}
	return newSHA, nil
}

func updateHead(root, oldHead, newHead string) error {
	cmd := exec.Command("git", gitArgs(root, "update-ref", "HEAD", newHead, oldHead)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git update-ref HEAD: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func gitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command("git", gitArgs(root, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func gitArgs(root string, args ...string) []string {
	out := []string{"-c", "safe.directory=" + root, "-C", root}
	return append(out, args...)
}

func lenCommitTargets(targets map[string][]rules.Rule) int {
	var n int
	for _, rs := range targets {
		n += len(rs)
	}
	return n
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

func appendGitignore(root string, patterns map[string]struct{}, preview bool) (int, error) {
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
	if preview {
		return len(missing), nil
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

func untrack(root string, paths map[string]struct{}, preview bool) (int, error) {
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
		check := exec.Command("git", gitArgs(root, "ls-files", "--error-unmatch", "--", p)...)
		if err := check.Run(); err != nil {
			continue
		}
		if preview {
			fixed++
			continue
		}
		cmd := exec.Command("git", gitArgs(root, "rm", "--cached", "--ignore-unmatch", "--", p)...)
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
