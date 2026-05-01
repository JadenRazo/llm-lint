package scanner_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/rules"
	"github.com/JadenRazo/llm-lint/internal/scanner"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

// The walker is concurrent (saracen/walker) and matches accumulate behind a
// mutex inside scanner.Scan. This test seeds enough files to exercise the
// concurrent path under the race detector. CI runs with -race so this test
// is the primary guard against future data-race regressions in the result
// accumulator or stats counters.
func TestScanner_Concurrency_LargeTree_NoRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-tree stress under -short")
	}
	t.Parallel()
	root := t.TempDir()

	const fileCount = 1500
	for i := 0; i < fileCount; i++ {
		dir := filepath.Join(root, fmt.Sprintf("d%03d", i/50))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := strings.Repeat("ok\n", 10)
		// Sprinkle in path-rule and content-rule hits so the result accumulator
		// is touched from many goroutines, not just walked over.
		switch i % 100 {
		case 7:
			body = "package x\n# as an AI language model placeholder\n"
		case 23:
			// LLM005: matches CLAUDE_*.md
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("CLAUDE_NOTE_%03d.md", i)), []byte("scratch\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d.py", i)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, stats, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesWalked < fileCount {
		t.Errorf("FilesWalked = %d, want >= %d (some files lost during concurrent walk?)", stats.FilesWalked, fileCount)
	}
	if len(matches) == 0 {
		t.Error("expected non-zero findings (refusal-string + scratchpad fixtures sprinkled)")
	}
}
