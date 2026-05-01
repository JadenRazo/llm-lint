package scanner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/rules"
	"github.com/JadenRazo/llm-lint/internal/scanner"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

// scanner.go pins maxFileSize=5MB and maxContentSize=2MB. These boundaries
// matter: a regression that drops them would degrade content-rule coverage
// silently; a regression that raises them could blow up scan time on huge files.
const (
	mb            = 1024 * 1024
	maxFileBytes  = 5 * mb
	maxContent    = 2 * mb
	refusalString = "as an AI language model"
)

func writeFile(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanner_FileSizeBoundary_LargeFileSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// CLAUDE.md just over maxFileSize: should be entirely skipped (path rule
	// won't fire either, since the walker bails before recording).
	big := make([]byte, maxFileBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	writeFile(t, root, "CLAUDE.md", big)

	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, _, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if m.Rule.ID == "LLM001" {
			t.Errorf("CLAUDE.md > maxFileSize must be skipped entirely; got match %v", m)
		}
	}
}

func TestScanner_FileSizeBoundary_ExactlyAtMaxFileSize(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	exact := make([]byte, maxFileBytes)
	for i := range exact {
		exact[i] = 'a'
	}
	writeFile(t, root, "CLAUDE.md", exact)

	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, _, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	for _, m := range matches {
		if m.Rule.ID == "LLM001" {
			hit = true
		}
	}
	if !hit {
		t.Error("CLAUDE.md at exactly maxFileSize should still match the path rule")
	}
}

func TestScanner_ContentSizeBoundary_OverMaxContentSkipsContentRules(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// File over maxContentSize but under maxFileSize: path rules should still
	// fire on the path; content rules should NOT scan the body.
	body := make([]byte, maxContent+1)
	for i := range body {
		body[i] = 'a'
	}
	// Embed a refusal string near the end where content rules would otherwise hit.
	copy(body[len(body)-len(refusalString):], refusalString)
	writeFile(t, root, "src/big.py", body)

	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, _, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if m.Rule.ID == "LLM013" {
			t.Errorf("content rule must not scan files > maxContentSize; got %v", m)
		}
	}
}

func TestScanner_ContentSizeBoundary_AtMaxContent_StillScans(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Aim for total size exactly == maxContent so we exercise the boundary
	// (info.Size() <= maxContentSize is the gate inside scanner.scanContent).
	padding := strings.Repeat("a", maxContent-len(refusalString)-2)
	body := padding + "\n" + refusalString + "\n"
	if len(body) != maxContent {
		t.Fatalf("test setup miscount: body=%d, want %d", len(body), maxContent)
	}
	writeFile(t, root, "src/edge.py", []byte(body))

	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, _, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	for _, m := range matches {
		if m.Rule.ID == "LLM013" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("content rule should fire on file <= maxContentSize; got matches=%v", ruleIDs(matches))
	}
}

func TestScanner_EmptyFile_NoCrash(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "src/empty.py", []byte{})

	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Scan(root); err != nil {
		t.Errorf("empty file should not error: %v", err)
	}
}
