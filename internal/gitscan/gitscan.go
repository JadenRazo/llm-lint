package gitscan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/JadenRazo/llm-lint/internal/progress"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

type Config interface {
	IsRuleEnabled(id string) bool
	EffectiveSeverity(id string, def rules.Severity) rules.Severity
	HistoryDepth() int
	Since() string
}

type Result struct {
	Matches        []rules.Match
	CommitsScanned int
	Shallow        bool
}

type Scanner struct {
	cfg            Config
	trailerRules   []compiledRule
	messageRules   []compiledRule
}

type compiledRule struct {
	rule     rules.Rule
	patterns []*regexp.Regexp
}

func New(allRules map[string]rules.Rule, cfg Config) *Scanner {
	s := &Scanner{cfg: cfg}
	for _, r := range allRules {
		if !cfg.IsRuleEnabled(r.ID) {
			continue
		}
		switch r.Kind {
		case rules.KindGitTrailer:
			s.trailerRules = append(s.trailerRules, compileRule(r, r.TrailerPatterns))
		case rules.KindGitMessage:
			s.messageRules = append(s.messageRules, compileRule(r, r.MessagePatterns))
		}
	}
	return s
}

func compileRule(r rules.Rule, patterns []string) compiledRule {
	out := compiledRule{rule: r}
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err == nil {
			out.patterns = append(out.patterns, re)
		}
	}
	return out
}

func (s *Scanner) Scan(root string) (*Result, error) {
	return s.ScanWithProgress(root, nil)
}

func (s *Scanner) ScanWithProgress(root string, prog *progress.Reporter) (*Result, error) {
	gitDir := filepath.Join(root, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Result{}, fmt.Errorf("not a git repository: %s", root)
		}
		return nil, err
	}

	repo, err := git.PlainOpen(root)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	res := &Result{}

	if shallow, err := isShallow(gitDir); err == nil && shallow {
		res.Shallow = true
	}

	head, err := repo.Head()
	if err != nil {
		return res, nil
	}

	var sinceHash *plumbing.Hash
	if since := s.cfg.Since(); since != "" {
		h, err := repo.ResolveRevision(plumbing.Revision(since))
		if err == nil {
			sinceHash = h
		}
	}

	iter, err := repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		return res, fmt.Errorf("log: %w", err)
	}
	defer iter.Close()

	depth := s.cfg.HistoryDepth()
	scanned := 0
	prog.SetCommits(0, depth)
	err = iter.ForEach(func(c *object.Commit) error {
		if sinceHash != nil && c.Hash == *sinceHash {
			return errStopIter
		}
		if depth > 0 && scanned >= depth {
			return errStopIter
		}
		scanned++
		s.applyToCommit(c, res)
		if scanned%16 == 0 {
			prog.SetCommits(scanned, depth)
		}
		return nil
	})
	prog.SetCommits(scanned, depth)
	if err != nil && !errors.Is(err, errStopIter) {
		return res, err
	}
	res.CommitsScanned = scanned
	return res, nil
}

var errStopIter = errors.New("stop")

func (s *Scanner) applyToCommit(c *object.Commit, res *Result) {
	msg := c.Message
	trailers := extractTrailers(msg)
	author := fmt.Sprintf("%s <%s>", c.Author.Name, c.Author.Email)
	sha := c.Hash.String()
	subject := firstLine(msg)

	for _, cr := range s.trailerRules {
		for _, line := range trailers {
			if matchesAny(line, cr.patterns) {
				res.Matches = append(res.Matches, rules.Match{
					Rule:      applySeverity(cr.rule, s.cfg),
					CommitSHA: sha,
					CommitMsg: subject,
					Author:    author,
					Snippet:   line,
				})
				break
			}
		}
	}
	for _, cr := range s.messageRules {
		if matchesAny(msg, cr.patterns) {
			res.Matches = append(res.Matches, rules.Match{
				Rule:      applySeverity(cr.rule, s.cfg),
				CommitSHA: sha,
				CommitMsg: subject,
				Author:    author,
			})
		}
	}
}

func applySeverity(r rules.Rule, cfg Config) rules.Rule {
	r.Severity = cfg.EffectiveSeverity(r.ID, r.Severity)
	return r
}

func matchesAny(s string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

var trailerLineRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*:\s`)

func extractTrailers(msg string) []string {
	msg = strings.TrimRight(msg, "\n")
	if msg == "" {
		return nil
	}
	paragraphs := strings.Split(msg, "\n\n")
	last := paragraphs[len(paragraphs)-1]
	lines := strings.Split(last, "\n")
	var out []string
	allTrailers := true
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		if !trailerLineRe.MatchString(l) {
			allTrailers = false
			break
		}
	}
	if allTrailers && len(paragraphs) > 1 {
		for _, l := range lines {
			out = append(out, strings.TrimRight(l, "\r"))
		}
		return out
	}
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		if trailerLineRe.MatchString(l) {
			out = append(out, l)
		}
	}
	return out
}

func firstLine(msg string) string {
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		return msg[:i]
	}
	return msg
}

func isShallow(gitDir string) (bool, error) {
	st, err := os.Stat(filepath.Join(gitDir, "shallow"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return st.Size() > 0, nil
}
