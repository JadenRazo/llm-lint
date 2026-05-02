package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

func runHook(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	cmd := newRoot()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return outBuf.Bytes(), err
}

func gitInit(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if _, err := git.PlainInit(d, false); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCLI_HookStatus_NotInstalled(t *testing.T) {
	root := gitInit(t)
	out, err := runHook(t, "hook", "status", root)
	if err != nil {
		t.Fatalf("hook status: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hook status: not-installed") {
		t.Errorf("expected 'hook status: not-installed'; got %q", out)
	}
	if !strings.Contains(string(out), "pre-commit framework:") {
		t.Errorf("expected pre-commit framework line; got %q", out)
	}
}

func TestCLI_HookInstall_Native(t *testing.T) {
	root := gitInit(t)
	out, err := runHook(t, "hook", "install", "--type", "native", root)
	if err != nil {
		t.Fatalf("hook install: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hook status: native") {
		t.Errorf("expected 'hook status: native'; got %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "hooks", "pre-commit")); err != nil {
		t.Fatalf("native hook not written: %v", err)
	}
}

func TestCLI_HookInstall_Auto_PicksFramework(t *testing.T) {
	root := gitInit(t)
	// Presence of .pre-commit-config.yaml should make autodetect pick framework.
	if err := os.WriteFile(filepath.Join(root, ".pre-commit-config.yaml"), []byte("repos: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runHook(t, "hook", "install", "--type", "auto", "--rev", "v0.1.0", root)
	if err != nil {
		t.Fatalf("hook install auto: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hook status: framework") {
		t.Errorf("expected framework state after auto-install with config present; got %q", out)
	}
}

func TestCLI_HookUninstall_Native(t *testing.T) {
	root := gitInit(t)
	if _, err := runHook(t, "hook", "install", "--type", "native", root); err != nil {
		t.Fatal(err)
	}
	out, err := runHook(t, "hook", "uninstall", "--type", "native", root)
	if err != nil {
		t.Fatalf("hook uninstall: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hook status: not-installed") {
		t.Errorf("expected 'not-installed' after uninstall; got %q", out)
	}
}

func TestCLI_HookUninstall_NothingInstalled(t *testing.T) {
	root := gitInit(t)
	out, err := runHook(t, "hook", "uninstall", root)
	if err != nil {
		t.Fatalf("hook uninstall on clean repo: %v", err)
	}
	if !strings.Contains(string(out), "nothing to uninstall") {
		t.Errorf("expected 'nothing to uninstall' note; got %q", out)
	}
}

func TestCLI_Hook_BareRunsStatus(t *testing.T) {
	root := gitInit(t)
	// `llm-lint hook` with no subcommand should behave like `hook status`.
	out, err := runHook(t, "hook", root)
	if err != nil {
		t.Fatalf("hook (no subcommand): %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hook status:") {
		t.Errorf("bare 'hook' should print status; got %q", out)
	}
}
