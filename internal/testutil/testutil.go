// Package testutil holds helpers shared across package test suites.
// It is imported only from _test.go files.
package testutil

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	git "github.com/go-git/go-git/v5"
)

// UpdateGolden is the shared -update flag: rewrite golden files with the
// current output instead of comparing against them. Registered once here so
// every test binary that uses CompareGolden accepts `go test -update`.
var UpdateGolden = flag.Bool("update", false, "rewrite golden files with current output")

// Itoa formats an int in base 10. Exists so test fixtures don't each
// hand-roll their own integer formatting.
func Itoa(i int) string {
	return strconv.Itoa(i)
}

// CompareGolden compares got against the golden file at path, failing the
// test with a readable diff on mismatch. With -update it rewrites the golden
// file (creating parent directories as needed) instead.
func CompareGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *UpdateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `go test -update` in this package to create it)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s mismatch.\n--- got ---\n%s\n--- want ---\n%s", filepath.Base(path), got, want)
	}
}

// InitRepo creates an empty git repository in a fresh temp directory and
// returns its root. The directory is cleaned up when the test ends.
func InitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := git.PlainInit(root, false); err != nil {
		t.Fatal(err)
	}
	return root
}
