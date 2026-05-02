package scanner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"golang.org/x/sync/errgroup"

	"github.com/JadenRazo/llm-lint/internal/progress"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

const indexWorkers = 16

// ScanIndex walks the git index instead of the working tree. It scans the
// staged blob bytes for content rules, so a user who fixed a file but did
// not stage the fix still sees the staged-bad version flagged. Trailer and
// message rules do not apply (no commit yet at pre-commit time) — the
// engine layer is responsible for not invoking gitscan in staged-only mode.
func (s *Scanner) ScanIndex(root string, prog *progress.Reporter) ([]rules.Match, Stats, error) {
	var stats Stats

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, stats, err
	}

	repo, err := git.PlainOpen(absRoot)
	if err != nil {
		return nil, stats, fmt.Errorf("open repo: %w", err)
	}

	if _, err := repo.Worktree(); err != nil {
		if errors.Is(err, git.ErrIsBareRepository) {
			return nil, stats, fmt.Errorf("staged-only requires a worktree (bare repo not supported)")
		}
		return nil, stats, fmt.Errorf("worktree: %w", err)
	}

	idx, err := repo.Storer.Index()
	if err != nil {
		return nil, stats, fmt.Errorf("read index: %w", err)
	}

	var (
		mu      sync.Mutex
		matches []rules.Match
	)

	g, ctx := errgroup.WithContext(context.Background())
	sem := make(chan struct{}, indexWorkers)

schedule:
	for _, e := range idx.Entries {
		if e.Mode != filemode.Regular && e.Mode != filemode.Executable {
			continue
		}
		entry := e

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break schedule
		}

		g.Go(func() error {
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return ctx.Err()
			}

			rel := entry.Name
			if s.cfg.IsIgnored(rel) {
				return nil
			}
			if int64(entry.Size) > maxFileSize {
				return nil
			}

			atomic.AddInt64(&stats.FilesWalked, 1)

			var local []rules.Match
			for _, r := range s.pathRules {
				if matchesAnyGlob(rel, r.PathGlobs) {
					local = append(local, rules.Match{
						Rule: applySeverity(r, s.cfg),
						Path: rel,
					})
				}
			}

			if len(s.contentRules) > 0 && int64(entry.Size) <= maxContentSize {
				blob, err := repo.BlobObject(entry.Hash)
				if err == nil {
					rc, err := blob.Reader()
					if err == nil {
						data, _ := io.ReadAll(rc)
						_ = rc.Close()
						local = append(local, s.applyContentRules(rel, data)...)
						atomic.AddInt64(&stats.BytesRead, int64(len(data)))
					}
				}
			}

			atomic.AddInt64(&stats.FilesScanned, 1)
			prog.IncFile()

			if len(local) > 0 {
				mu.Lock()
				matches = append(matches, local...)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return matches, stats, err
	}
	return matches, stats, nil
}
