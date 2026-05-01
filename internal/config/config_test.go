package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/config"
	"github.com/JadenRazo/llm-lint/internal/rules"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

func TestLoad_DefaultsWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(".llmlint.yaml", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.GitEnabled() {
		t.Error("git scan should default to enabled")
	}
	if !cfg.FilesystemEnabled() {
		t.Error("fs scan should default to enabled")
	}
	if cfg.HistoryDepth() != 1000 {
		t.Errorf("history depth default: got %d want 1000", cfg.HistoryDepth())
	}
	if cfg.FailOnRank() != rules.SevError.Rank() {
		t.Error("fail_on default should be error")
	}
}

func TestLoad_ParsesFile(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
ignore:
  - "vendor/**"
  - "testdata/**"
rules:
  LLM004:
    enabled: false
  LLM013:
    severity: warning
fail_on: warning
scan:
  git_history_depth: 50
`
	if err := os.WriteFile(filepath.Join(dir, ".llmlint.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(".llmlint.yaml", dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsRuleEnabled("LLM004") {
		t.Error("LLM004 should be disabled")
	}
	if !cfg.IsRuleEnabled("LLM001") {
		t.Error("LLM001 should still be enabled")
	}
	if got := cfg.EffectiveSeverity("LLM013", rules.SevInfo); got != rules.SevWarning {
		t.Errorf("LLM013 severity override: got %s want warning", got)
	}
	if !cfg.IsIgnored("vendor/foo/bar.go") {
		t.Error("vendor/foo/bar.go should be ignored")
	}
	if cfg.IsIgnored("src/main.go") {
		t.Error("src/main.go should not be ignored")
	}
	if cfg.HistoryDepth() != 50 {
		t.Errorf("depth: got %d want 50", cfg.HistoryDepth())
	}
}

func TestApplyCLIOverrides_NoGit(t *testing.T) {
	cfg, err := config.Load(".llmlint.yaml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyCLIOverrides(nil, nil, true, "")
	if cfg.GitEnabled() {
		t.Error("--no-git should disable git")
	}
}

func TestApplyCLIOverrides_IncludeExclude(t *testing.T) {
	cfg, err := config.Load(".llmlint.yaml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyCLIOverrides([]string{"LLM099"}, []string{"LLM001"}, false, "")
	if cfg.IsRuleEnabled("LLM001") {
		t.Error("LLM001 should be excluded")
	}
	if !cfg.IsRuleEnabled("LLM099") {
		t.Error("LLM099 should be force-included")
	}
}

func TestCategoriesFilter(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
categories:
  - claude
`
	if err := os.WriteFile(filepath.Join(dir, ".llmlint.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(".llmlint.yaml", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsRuleEnabled("LLM001") {
		t.Error("claude category rule should be enabled")
	}
	if cfg.IsRuleEnabled("LLM006") {
		t.Error("cursor category rule should be filtered out when only claude is in categories")
	}
}
