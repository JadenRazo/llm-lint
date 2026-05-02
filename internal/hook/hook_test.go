package hook_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"

	"github.com/JadenRazo/llm-lint/internal/hook"
)

func initRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if _, err := git.PlainInit(d, false); err != nil {
		t.Fatal(err)
	}
	return d
}

func nativePath(root string) string {
	return filepath.Join(root, ".git", "hooks", "pre-commit")
}

func frameworkPath(root string) string {
	return filepath.Join(root, ".pre-commit-config.yaml")
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestInstall_NativeOnCleanRepo(t *testing.T) {
	root := initRepo(t)
	st, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeNative, BinaryVersion: "0.1.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != hook.StateNative {
		t.Errorf("state: got %q want native", st.State)
	}
	body := read(t, nativePath(root))
	if !strings.Contains(body, hook.MarkerStart) || !strings.Contains(body, hook.MarkerEnd) {
		t.Errorf("hook should have markers; got\n%s", body)
	}
	if !strings.Contains(body, "--staged-only") {
		t.Errorf("hook should invoke --staged-only; got\n%s", body)
	}
	info, err := os.Stat(nativePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("hook must be executable; mode=%v", info.Mode())
	}
}

func TestInstall_NativeIdempotent(t *testing.T) {
	root := initRepo(t)
	if _, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeNative, BinaryVersion: "0.1.2",
	}); err != nil {
		t.Fatal(err)
	}
	first := read(t, nativePath(root))
	st, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeNative, BinaryVersion: "0.1.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	second := read(t, nativePath(root))
	if first != second {
		t.Errorf("second install must not change managed-hook bytes")
	}
	hasNote := false
	for _, n := range st.Notes {
		if strings.Contains(n, "already installed") {
			hasNote = true
		}
	}
	if !hasNote {
		t.Errorf("expected 'already installed' note; got %v", st.Notes)
	}
}

func TestInstall_RefusesForeignNativeWithoutForce(t *testing.T) {
	root := initRepo(t)
	if err := os.MkdirAll(filepath.Join(root, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "#!/usr/bin/env bash\necho \"my custom hook\"\n"
	if err := os.WriteFile(nativePath(root), []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeNative, BinaryVersion: "0.1.2",
	})
	if err == nil {
		t.Errorf("expected refusal on foreign hook")
	}
	if got := read(t, nativePath(root)); got != foreign {
		t.Errorf("foreign hook must remain untouched")
	}
}

func TestInstall_OverwritesForeignNativeWithForce(t *testing.T) {
	root := initRepo(t)
	if err := os.MkdirAll(filepath.Join(root, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "#!/usr/bin/env bash\necho \"old\"\n"
	if err := os.WriteFile(nativePath(root), []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeNative, Force: true, BinaryVersion: "0.1.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != hook.StateNative {
		t.Errorf("state: got %q want native", st.State)
	}
	if !strings.Contains(read(t, nativePath(root)), hook.MarkerStart) {
		t.Errorf("forced overwrite should produce managed hook")
	}
}

func TestUninstall_NativeRemovesManagedBlock(t *testing.T) {
	root := initRepo(t)
	if _, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeNative, BinaryVersion: "0.1.2",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := hook.Uninstall(root, hook.ModeNative); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(nativePath(root)); !os.IsNotExist(err) {
		t.Errorf("uninstall on shebang-only managed file should remove it; err=%v", err)
	}
}

func TestUninstall_PreservesForeignContentAroundMarkers(t *testing.T) {
	root := initRepo(t)
	if err := os.MkdirAll(filepath.Join(root, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Hand-edited hook with managed block in the middle.
	content := "#!/usr/bin/env bash\necho \"team hook prelude\"\n" +
		hook.MarkerStart + "\nexec llm-lint scan --staged-only\n" + hook.MarkerEnd + "\n" +
		"echo \"team hook postlude\"\n"
	if err := os.WriteFile(nativePath(root), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := hook.Uninstall(root, hook.ModeNative); err != nil {
		t.Fatal(err)
	}
	got := read(t, nativePath(root))
	if strings.Contains(got, hook.MarkerStart) || strings.Contains(got, hook.MarkerEnd) {
		t.Errorf("markers must be removed; got\n%s", got)
	}
	if !strings.Contains(got, "team hook prelude") || !strings.Contains(got, "team hook postlude") {
		t.Errorf("user content must be preserved; got\n%s", got)
	}
}

func TestUninstall_NothingInstalled_NoOp(t *testing.T) {
	root := initRepo(t)
	st, err := hook.Uninstall(root, hook.ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != hook.StateNotInstalled {
		t.Errorf("state: got %q want not-installed", st.State)
	}
}

func TestInstall_FrameworkOnEmptyConfig(t *testing.T) {
	root := initRepo(t)
	st, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, BinaryVersion: "0.1.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != hook.StateFramework {
		t.Errorf("state: got %q want framework", st.State)
	}
	body := read(t, frameworkPath(root))
	if !strings.Contains(body, hook.RepoURL) {
		t.Errorf("config must reference llm-lint repo URL; got\n%s", body)
	}
	if !strings.Contains(body, "rev: v0.1.2") {
		t.Errorf("rev must be v0.1.2; got\n%s", body)
	}
	if !strings.Contains(body, "id: llm-lint") {
		t.Errorf("hooks list must include id: llm-lint; got\n%s", body)
	}
}

func TestInstall_FrameworkIdempotentSameRev(t *testing.T) {
	root := initRepo(t)
	if _, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, BinaryVersion: "0.1.2",
	}); err != nil {
		t.Fatal(err)
	}
	first := read(t, frameworkPath(root))
	st, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, BinaryVersion: "0.1.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	second := read(t, frameworkPath(root))
	if first != second {
		t.Errorf("second framework install at same rev must not change file")
	}
	hasNote := false
	for _, n := range st.Notes {
		if strings.Contains(n, "already installed") {
			hasNote = true
		}
	}
	if !hasNote {
		t.Errorf("expected 'already installed' note; got %v", st.Notes)
	}
}

func TestInstall_FrameworkRevUpdatePreservesComments(t *testing.T) {
	root := initRepo(t)
	initial := `# top comment
repos:
  # another tool
  - repo: https://github.com/some/other
    rev: v1.0
    hooks:
      - id: other-hook
  # llm-lint comment
  - repo: https://github.com/JadenRazo/llm-lint
    rev: v0.1.0
    hooks:
      - id: llm-lint
  # tail comment
`
	if err := os.WriteFile(frameworkPath(root), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, BinaryVersion: "0.2.0",
	}); err != nil {
		t.Fatal(err)
	}

	got := read(t, frameworkPath(root))
	if !strings.Contains(got, "rev: v0.2.0") {
		t.Errorf("rev must be updated to v0.2.0; got\n%s", got)
	}
	for _, want := range []string{"# top comment", "# another tool", "# llm-lint comment", "# tail comment", "rev: v1.0"} {
		if !strings.Contains(got, want) {
			t.Errorf("must preserve %q; got\n%s", want, got)
		}
	}
}

func TestInstall_FrameworkAppendsToExistingConfig(t *testing.T) {
	root := initRepo(t)
	initial := `repos:
  - repo: https://github.com/some/other
    rev: v1.0
    hooks:
      - id: other-hook
`
	if err := os.WriteFile(frameworkPath(root), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, BinaryVersion: "0.2.0",
	}); err != nil {
		t.Fatal(err)
	}
	got := read(t, frameworkPath(root))
	if !strings.Contains(got, "id: other-hook") {
		t.Errorf("must preserve existing entry; got\n%s", got)
	}
	if !strings.Contains(got, "id: llm-lint") {
		t.Errorf("must append llm-lint entry; got\n%s", got)
	}
}

func TestInstall_FrameworkMultiDocRefused(t *testing.T) {
	root := initRepo(t)
	multi := "---\nrepos: []\n---\nrepos: []\n"
	if err := os.WriteFile(frameworkPath(root), []byte(multi), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, BinaryVersion: "0.1.2",
	})
	if err == nil || !strings.Contains(err.Error(), "multi-document") {
		t.Errorf("expected multi-document refusal; got %v", err)
	}
}

func TestInstall_RefusesMovingRevWithoutOptIn(t *testing.T) {
	root := initRepo(t)
	_, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, Rev: "HEAD", BinaryVersion: "0.1.2",
	})
	if err == nil || !strings.Contains(err.Error(), "moving") {
		t.Errorf("expected refusal for --rev HEAD; got %v", err)
	}
}

func TestInstall_AllowsMovingRevWithOptIn(t *testing.T) {
	root := initRepo(t)
	st, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, Rev: "main", AllowMovingRev: true, BinaryVersion: "0.1.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != hook.StateFramework {
		t.Errorf("state: got %q want framework", st.State)
	}
	if !strings.Contains(read(t, frameworkPath(root)), "rev: main") {
		t.Errorf("config must contain rev: main")
	}
}

func TestInstall_DevBinaryFallsBackToZeroVersion(t *testing.T) {
	root := initRepo(t)
	st, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, BinaryVersion: "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, frameworkPath(root)), "rev: v0.0.0") {
		t.Errorf("dev binary should pin to v0.0.0")
	}
	hasWarn := false
	for _, n := range st.Notes {
		if strings.Contains(n, "v0.0.0") {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Errorf("expected dev-build warning note; got %v", st.Notes)
	}
}

func TestStatus_DetectsNotInstalled(t *testing.T) {
	root := initRepo(t)
	st, err := hook.GetStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != hook.StateNotInstalled {
		t.Errorf("state: got %q want not-installed", st.State)
	}
}

func TestStatus_DetectsBoth(t *testing.T) {
	root := initRepo(t)
	if _, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeNative, BinaryVersion: "0.1.2",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, BinaryVersion: "0.1.2",
	}); err != nil {
		t.Fatal(err)
	}
	st, err := hook.GetStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != hook.StateBoth {
		t.Errorf("state: got %q want both", st.State)
	}
}

func TestStatus_DetectsForeign(t *testing.T) {
	root := initRepo(t)
	if err := os.MkdirAll(filepath.Join(root, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativePath(root), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := hook.GetStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != hook.StateForeign {
		t.Errorf("state: got %q want foreign", st.State)
	}
}

func TestUninstall_BothRequiresType(t *testing.T) {
	root := initRepo(t)
	if _, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeNative, BinaryVersion: "0.1.2",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, BinaryVersion: "0.1.2",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := hook.Uninstall(root, hook.ModeAuto)
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("expected refusal mentioning 'both'; got %v", err)
	}
}

func TestUninstall_FrameworkRemovesEntry(t *testing.T) {
	root := initRepo(t)
	if _, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, BinaryVersion: "0.1.2",
	}); err != nil {
		t.Fatal(err)
	}
	st, err := hook.Uninstall(root, hook.ModeFramework)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != hook.StateNotInstalled {
		t.Errorf("state after framework uninstall: got %q want not-installed", st.State)
	}
	body := read(t, frameworkPath(root))
	if strings.Contains(body, hook.RepoURL) {
		t.Errorf("config must not reference llm-lint repo after uninstall; got\n%s", body)
	}
}

func TestUninstall_FrameworkPreservesOtherRepos(t *testing.T) {
	root := initRepo(t)
	existing := `repos:
  - repo: https://github.com/pre-commit/pre-commit-hooks
    rev: v4.0.0
    hooks:
      - id: trailing-whitespace
`
	if err := os.WriteFile(frameworkPath(root), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := hook.Install(hook.InstallOptions{
		RepoRoot: root, Mode: hook.ModeFramework, BinaryVersion: "0.1.2",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := hook.Uninstall(root, hook.ModeFramework); err != nil {
		t.Fatal(err)
	}
	body := read(t, frameworkPath(root))
	if !strings.Contains(body, "pre-commit-hooks") {
		t.Errorf("uninstall must preserve unrelated repo entries; got\n%s", body)
	}
	if strings.Contains(body, hook.RepoURL) {
		t.Errorf("llm-lint block should be gone; got\n%s", body)
	}
	if !strings.Contains(body, "id: trailing-whitespace") {
		t.Errorf("unrelated hook entry should remain; got\n%s", body)
	}
}

func TestUninstall_FrameworkNoConfig_NoError(t *testing.T) {
	root := initRepo(t)
	// No .pre-commit-config.yaml present at all.
	st, err := hook.Uninstall(root, hook.ModeFramework)
	if err != nil {
		t.Fatalf("framework uninstall on missing config should be no-op; got %v", err)
	}
	if st.State != hook.StateNotInstalled {
		t.Errorf("state: got %q want not-installed", st.State)
	}
}

func TestInstall_NotARepo_Errors(t *testing.T) {
	d := t.TempDir()
	_, err := hook.Install(hook.InstallOptions{
		RepoRoot: d, Mode: hook.ModeNative, BinaryVersion: "0.1.2",
	})
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("expected 'not a git repository'; got %v", err)
	}
}
