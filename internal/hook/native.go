package hook

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// nativeScript is the canonical managed hook body. Markers MUST appear on
// their own lines for inspect/remove to work; do not change without
// updating MarkerStart/MarkerEnd.
const nativeScript = `#!/usr/bin/env bash
` + MarkerStart + `
# This block is managed by ` + "`llm-lint hook install`" + `. Do not edit between
# the start/end markers — they are how ` + "`llm-lint hook uninstall`" + ` finds
# the block to remove. Hand-edit anything outside this block freely.
set -euo pipefail
if ! command -v llm-lint >/dev/null 2>&1; then
  echo "llm-lint: binary not on PATH; skipping pre-commit scan" >&2
  exit 0
fi
exec llm-lint scan --staged-only --no-git --fail-on error
` + MarkerEnd + `
`

func nativeHookPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".git", "hooks", "pre-commit")
}

func hooksDir(repoRoot string) string {
	return filepath.Join(repoRoot, ".git", "hooks")
}

// inspectNative reports whether a managed-native hook exists, and whether a
// foreign (non-managed) hook exists in its place.
func inspectNative(repoRoot string) (managed, foreign bool, err error) {
	dir := hooksDir(repoRoot)
	if st, err := os.Lstat(dir); err == nil && !st.IsDir() {
		return false, false, fmt.Errorf("expected directory at %s", dir)
	} else if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, false, nil
		}
		return false, false, err
	}

	path := nativeHookPath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, false, nil
		}
		return false, false, err
	}
	if bytes.Contains(data, []byte(MarkerStart)) {
		return true, false, nil
	}
	return false, true, nil
}

func installNative(opts InstallOptions) (Status, error) {
	repoRoot := opts.RepoRoot
	dir := hooksDir(repoRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Status{}, fmt.Errorf("create hooks dir: %w", err)
	}

	path := nativeHookPath(repoRoot)
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Status{}, err
	}

	st := Status{NativePath: path, PreCommitOnPath: hasPreCommitFramework()}

	if len(existing) == 0 {
		if err := writeExec(path, []byte(nativeScript)); err != nil {
			return st, err
		}
		st.State = StateNative
		return st, nil
	}

	if bytes.Contains(existing, []byte(MarkerStart)) {
		// Already managed; rewrite (cheap idempotency: same content if version unchanged).
		if bytes.Equal(existing, []byte(nativeScript)) {
			st.State = StateNative
			st.Notes = append(st.Notes, "already installed")
			return st, nil
		}
		if err := writeExec(path, []byte(nativeScript)); err != nil {
			return st, err
		}
		st.State = StateNative
		st.Notes = append(st.Notes, "updated existing managed hook")
		return st, nil
	}

	// Foreign hook present.
	if !opts.Force {
		st.State = StateForeign
		return st, errors.New("foreign pre-commit hook present (no managed markers); pass --force to overwrite, or use --type framework")
	}
	if err := writeExec(path, []byte(nativeScript)); err != nil {
		return st, err
	}
	st.State = StateNative
	st.Notes = append(st.Notes, "overwrote foreign pre-commit hook")
	return st, nil
}

// uninstallNative removes the managed block from the hook. If only the
// shebang and whitespace remain after removal, the entire file is deleted.
// Hand-written content outside the marker block is preserved.
func uninstallNative(repoRoot string) error {
	path := nativeHookPath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !bytes.Contains(data, []byte(MarkerStart)) {
		return nil
	}

	out := stripMarkerBlock(string(data))
	out = strings.TrimRight(out, "\n") + "\n"

	// Only shebang + whitespace remaining? Just delete the file.
	if isShebangOnly(out) {
		return os.Remove(path)
	}
	return writeExec(path, []byte(out))
}

// stripMarkerBlock removes everything from MarkerStart through MarkerEnd
// inclusive, plus the leading newline before MarkerStart if any.
func stripMarkerBlock(s string) string {
	startIdx := strings.Index(s, MarkerStart)
	if startIdx < 0 {
		return s
	}
	endRel := strings.Index(s[startIdx:], MarkerEnd)
	if endRel < 0 {
		return s // malformed; bail without changes
	}
	endIdx := startIdx + endRel + len(MarkerEnd)
	// Consume trailing newline after MarkerEnd if present.
	if endIdx < len(s) && s[endIdx] == '\n' {
		endIdx++
	}
	// Consume the newline immediately before MarkerStart so we don't
	// leave a doubled blank line.
	cutStart := startIdx
	if cutStart > 0 && s[cutStart-1] == '\n' {
		cutStart--
	}
	return s[:cutStart] + s[endIdx:]
}

// isShebangOnly returns true if the content is just a shebang (or empty +
// whitespace) — used to decide whether to delete the file after marker
// block removal.
func isShebangOnly(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(trimmed, "#!") {
		// First line is the shebang; check remainder.
		idx := strings.Index(trimmed, "\n")
		if idx < 0 {
			return true
		}
		return strings.TrimSpace(trimmed[idx:]) == ""
	}
	return false
}

func writeExec(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o755); err != nil {
		return err
	}
	// WriteFile respects umask; force the mode explicitly.
	return os.Chmod(path, 0o755)
}
