package scanner_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"

	"github.com/JadenRazo/llm-lint/internal/rules"
	"github.com/JadenRazo/llm-lint/internal/scanner"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

// initRepo creates an empty repo at root.
func initRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := git.PlainInit(root, false); err != nil {
		t.Fatal(err)
	}
	return root
}

// writeAndStage writes content at root/rel and `git add`s it.
func writeAndStage(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainOpen(root)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(rel); err != nil {
		t.Fatalf("add %s: %v", rel, err)
	}
}

func runIndex(t *testing.T, root string, cfg scanner.Config) []rules.Match {
	t.Helper()
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, _, err := s.ScanIndex(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func matchIDsByPath(matches []rules.Match) []string {
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.Rule.ID+":"+m.Path)
	}
	sort.Strings(out)
	return out
}

func TestScanIndex_StagedFlagged_UnstagedNot(t *testing.T) {
	root := initRepo(t)
	writeAndStage(t, root, "CLAUDE.md", "context\n")
	if err := os.WriteFile(filepath.Join(root, ".cursorrules"), []byte("rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := matchIDsByPath(runIndex(t, root, &testCfg{}))
	want := []string{"LLM001:CLAUDE.md"}
	if !equal(got, want) {
		t.Errorf("staged-only must scan only index entries\n got=%v\nwant=%v", got, want)
	}
}

func TestScanIndex_StagedContentBeatsWorkingTree(t *testing.T) {
	// The whole point of staged-only: the staged blob is what gets scanned,
	// regardless of working-tree edits the user has not staged yet.
	root := initRepo(t)
	writeAndStage(t, root, "src/handler.py", "def f():\n    # As an AI language model, I cannot do that\n    pass\n")
	if err := os.WriteFile(filepath.Join(root, "src/handler.py"), []byte("def f(): pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	matches := runIndex(t, root, &testCfg{})
	var hit *rules.Match
	for i, m := range matches {
		if m.Rule.ID == "LLM013" {
			hit = &matches[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("LLM013 should fire on staged blob; got matches=%v", matchIDsByPath(matches))
	}
	if hit.Path != "src/handler.py" {
		t.Errorf("path: got %q want src/handler.py", hit.Path)
	}
}

func TestScanIndex_DeletedFromWorktreeStillScanned(t *testing.T) {
	root := initRepo(t)
	writeAndStage(t, root, "CLAUDE.md", "context\n")
	if err := os.Remove(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	got := matchIDsByPath(runIndex(t, root, &testCfg{}))
	want := []string{"LLM001:CLAUDE.md"}
	if !equal(got, want) {
		t.Errorf("staged file deleted from worktree should still be scanned\n got=%v\nwant=%v", got, want)
	}
}

func TestScanIndex_BinaryFileNotFlaggedForContentRules(t *testing.T) {
	root := initRepo(t)
	binary := []byte("\x00\x00binary garbage with the phrase as an AI language model embedded\x00")
	writeAndStage(t, root, "src/data.bin", string(binary))

	for _, m := range runIndex(t, root, &testCfg{}) {
		if m.Rule.ID == "LLM013" {
			t.Errorf("binary blob in index should not trigger content rules; got %v", m)
		}
	}
}

func TestScanIndex_LargeFileSkipped(t *testing.T) {
	root := initRepo(t)
	big := bytes.Repeat([]byte("x"), 6*1024*1024)
	writeAndStage(t, root, "huge.txt", string(big))
	writeAndStage(t, root, "CLAUDE.md", "context\n")

	got := matchIDsByPath(runIndex(t, root, &testCfg{}))
	if !contains_(got, "LLM001:CLAUDE.md") {
		t.Errorf("expected LLM001:CLAUDE.md in matches; got %v", got)
	}
}

func TestScanIndex_NoCommitsYet(t *testing.T) {
	// Pre-first-commit repos must work — this is the common pre-commit-hook case.
	root := initRepo(t)
	writeAndStage(t, root, "CLAUDE.md", "context\n")

	got := matchIDsByPath(runIndex(t, root, &testCfg{}))
	want := []string{"LLM001:CLAUDE.md"}
	if !equal(got, want) {
		t.Errorf("staged-only must work pre-first-commit\n got=%v\nwant=%v", got, want)
	}
}

func TestScanIndex_NotARepo(t *testing.T) {
	dir := t.TempDir()
	s, err := scanner.New(&testCfg{}, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ScanIndex(dir, nil); err == nil {
		t.Errorf("expected error on non-git dir")
	}
}

func TestScanIndex_BareRepoRefused(t *testing.T) {
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, true); err != nil {
		t.Fatal(err)
	}
	s, err := scanner.New(&testCfg{}, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.ScanIndex(dir, nil)
	if err == nil || !strings.Contains(err.Error(), "bare") {
		t.Errorf("bare repo should error mentioning 'bare'; got %v", err)
	}
}

func TestScanIndex_HonorsIgnoreList(t *testing.T) {
	root := initRepo(t)
	writeAndStage(t, root, "vendor/x/CLAUDE.md", "context\n")
	writeAndStage(t, root, "src/CLAUDE.md", "context\n")

	cfg := &testCfg{ignore: []string{"vendor/x/CLAUDE.md"}}
	got := matchIDsByPath(runIndex(t, root, cfg))
	want := []string{"LLM001:src/CLAUDE.md"}
	if !equal(got, want) {
		t.Errorf("ignore list must apply in staged-only mode\n got=%v\nwant=%v", got, want)
	}
}

func TestScanIndex_Concurrency(t *testing.T) {
	// Stage 25 directories each with CLAUDE.md (matches LLM001 glob) plus
	// 25 unrelated clean files. Run under -race; mismatches indicate
	// data races in the worker pool.
	root := initRepo(t)
	for i := 0; i < 25; i++ {
		writeAndStage(t, root, "d"+itoa_(i)+"/CLAUDE.md", "context\n")
		writeAndStage(t, root, "d"+itoa_(i)+"/clean.go", "package x\n")
	}

	got := runIndex(t, root, &testCfg{})
	if len(got) != 25 {
		t.Errorf("expected 25 LLM001 hits across staged CLAUDE.md files; got %d", len(got))
	}
}

func itoa_(i int) string {
	if i == 0 {
		return "0"
	}
	out := ""
	for i > 0 {
		out = string(rune('0'+i%10)) + out
		i /= 10
	}
	return out
}

func contains_(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
