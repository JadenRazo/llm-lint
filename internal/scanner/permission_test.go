package scanner_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/rules"
	"github.com/JadenRazo/llm-lint/internal/scanner"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

// scanner.go silently swallows permission-denied and not-exist errors via the
// walker error callback. This is intentional — partially-readable trees in CI
// or restricted user dirs must not abort the scan. These tests pin that
// contract: an unreadable file in the tree must not fail the whole scan, and
// other findings in the same tree must still surface.

func TestScanner_PermissionDenied_DoesNotFailScan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode 0o000 has different semantics on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod 0o000; rerun as a non-root user to exercise this path")
	}
	t.Parallel()
	root := t.TempDir()

	// A clearly-flagged path-rule file that should still surface.
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// An unreadable file that the scanner must skip silently.
	noRead := filepath.Join(root, "src", "secret.py")
	if err := os.MkdirAll(filepath.Dir(noRead), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noRead, []byte("nope"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(noRead, 0o644) })

	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	matches, _, err := s.Scan(root)
	if err != nil {
		t.Errorf("scan must not return error on unreadable file in tree; got %v", err)
	}
	hit := false
	for _, m := range matches {
		if m.Rule.ID == "LLM001" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("scanner skipped readable findings when an unreadable sibling existed; got %v", ruleIDs(matches))
	}
}

func TestScanner_NonExistentRoot_ReturnsError(t *testing.T) {
	t.Parallel()
	cfg := &testCfg{}
	s, err := scanner.New(cfg, rules.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Scan(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Error("scanning a non-existent root should error (only mid-walk errors are swallowed)")
	}
}
