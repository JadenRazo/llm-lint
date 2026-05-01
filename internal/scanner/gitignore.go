package scanner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// gitignoreMatcher wraps a gitignore.Matcher with the metadata we need to
// translate scanner-relative slash-paths into the path components the matcher
// expects.
type gitignoreMatcher struct {
	m gitignore.Matcher
}

// loadGitignoreMatcher returns a matcher seeded from the .git/info/exclude file
// and every .gitignore in the tree rooted at absRoot. Returns nil if absRoot is
// not a git repository (no .git entry at the root). On read errors we still
// return a best-effort matcher with whatever patterns we did parse — failing
// open is preferable to crashing the scan over a malformed .gitignore.
func loadGitignoreMatcher(absRoot string) *gitignoreMatcher {
	if _, err := os.Stat(filepath.Join(absRoot, ".git")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return nil
	}

	fs := osfs.New(absRoot)
	patterns, _ := gitignore.ReadPatterns(fs, nil)
	if len(patterns) == 0 {
		return nil
	}
	return &gitignoreMatcher{m: gitignore.NewMatcher(patterns)}
}

// match reports whether the given repo-relative slash-path is ignored by
// any in-tree .gitignore. Match decisions follow git's own precedence
// (later patterns override earlier ones, including negations).
//
// The matcher only consults .gitignore files in the working tree. A file
// that's tracked despite being gitignored (the rare `git add -f` case) is
// still skipped here — accept that as a known simplification; it keeps the
// implementation free of the index round-trip.
func (g *gitignoreMatcher) match(relSlash string, isDir bool) bool {
	if g == nil || relSlash == "" || relSlash == "." {
		return false
	}
	parts := strings.Split(relSlash, "/")
	return g.m.Match(parts, isDir)
}
