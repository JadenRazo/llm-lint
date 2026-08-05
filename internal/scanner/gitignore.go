package scanner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5/osfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// gitignoreMatcher wraps a gitignore.Matcher with the metadata we need to
// translate scanner-relative slash-paths into the path components the matcher
// expects.
type gitignoreMatcher struct {
	m            gitignore.Matcher
	trackedFiles map[string]struct{}
	trackedDirs  map[string]struct{}
}

// loadGitignoreMatcher returns a matcher seeded from the .git/info/exclude file
// and every .gitignore in the tree rooted at absRoot. Returns nil if absRoot is
// not a git repository (no .git entry at the root). On read errors we still
// return a best-effort matcher with whatever patterns we did parse; failing
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
	tracked, trackedDirs := loadTrackedIndex(absRoot)
	return &gitignoreMatcher{
		m:            gitignore.NewMatcher(patterns),
		trackedFiles: tracked,
		trackedDirs:  trackedDirs,
	}
}

// match reports whether the given repo-relative slash-path is ignored by
// any in-tree .gitignore. Match decisions follow git's own precedence
// (later patterns override earlier ones, including negations).
//
// The matcher consults .gitignore for untracked working-tree noise, but callers
// can use tracked()/hasTrackedDescendant() to avoid hiding artifacts that are
// already in the index despite matching an ignore rule.
func (g *gitignoreMatcher) match(relSlash string, isDir bool) bool {
	if g == nil || relSlash == "" || relSlash == "." {
		return false
	}
	parts := strings.Split(relSlash, "/")
	return g.m.Match(parts, isDir)
}

func (g *gitignoreMatcher) isTracked(relSlash string) bool {
	if g == nil {
		return false
	}
	_, ok := g.trackedFiles[relSlash]
	return ok
}

func (g *gitignoreMatcher) hasTrackedDescendant(relSlash string) bool {
	if g == nil {
		return false
	}
	_, ok := g.trackedDirs[strings.TrimSuffix(relSlash, "/")]
	return ok
}

func loadTrackedIndex(absRoot string) (map[string]struct{}, map[string]struct{}) {
	tracked := map[string]struct{}{}
	trackedDirs := map[string]struct{}{}

	repo, err := git.PlainOpen(absRoot)
	if err != nil {
		return tracked, trackedDirs
	}
	idx, err := repo.Storer.Index()
	if err != nil {
		return tracked, trackedDirs
	}
	for _, e := range idx.Entries {
		name := filepath.ToSlash(e.Name)
		if name == "" {
			continue
		}
		tracked[name] = struct{}{}
		parts := strings.Split(name, "/")
		for i := 1; i < len(parts); i++ {
			trackedDirs[strings.Join(parts[:i], "/")] = struct{}{}
		}
	}
	return tracked, trackedDirs
}
