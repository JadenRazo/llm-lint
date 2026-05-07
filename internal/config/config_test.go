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
	if cfg.FailOn != rules.SevError {
		t.Errorf("fail_on default should be error, got %q", cfg.FailOn)
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
fix:
  git_history: scanned
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
	if cfg.FixGitHistory() != "scanned" {
		t.Errorf("fix.git_history: got %q want scanned", cfg.FixGitHistory())
	}
}

func TestApplyCLIOverrides_NoGit(t *testing.T) {
	cfg, err := config.Load(".llmlint.yaml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ApplyCLIOverrides(config.CLIOverrides{NoGit: true}); err != nil {
		t.Fatal(err)
	}
	if cfg.GitEnabled() {
		t.Error("--no-git should disable git")
	}
}

func TestApplyCLIOverrides_IncludeExclude(t *testing.T) {
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
	if err := cfg.ApplyCLIOverrides(config.CLIOverrides{
		Includes: []string{"llm006"},
		Excludes: []string{"LLM001"},
	}); err != nil {
		t.Fatal(err)
	}
	if cfg.IsRuleEnabled("LLM001") {
		t.Error("LLM001 should be excluded")
	}
	if !cfg.IsRuleEnabled("LLM006") {
		t.Error("LLM006 should be force-included even though category filtering would exclude it")
	}
}

func TestApplyCLIOverrides_BaselineFlags(t *testing.T) {
	cfg, err := config.Load(".llmlint.yaml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.BaselineEnabled() {
		t.Error("baseline should default to enabled")
	}
	if cfg.BaselinePath() != "" {
		t.Errorf("default BaselinePath should be empty; got %q", cfg.BaselinePath())
	}
	if cfg.BaselineStaleAction() != "warn" {
		t.Errorf("default stale_action should be warn; got %q", cfg.BaselineStaleAction())
	}
	if !cfg.BaselineIncludeSnippets() {
		t.Error("include_snippets should default to true")
	}
	if cfg.Since() != "" || cfg.StagedOnly() {
		t.Error("Since/StagedOnly should default to zero")
	}
	if cfg.FixGitHistory() != "latest" {
		t.Errorf("FixGitHistory default: got %q want latest", cfg.FixGitHistory())
	}

	if err := cfg.ApplyCLIOverrides(config.CLIOverrides{
		Since:             "HEAD~5",
		BaselinePath:      "custom-baseline.yaml",
		NoBaseline:        true,
		BaselineStaleFail: true,
		FixGitHistory:     "none",
	}); err != nil {
		t.Fatal(err)
	}
	if cfg.Since() != "HEAD~5" {
		t.Errorf("Since: got %q want HEAD~5", cfg.Since())
	}
	if cfg.BaselineEnabled() {
		t.Error("--no-baseline should disable baseline")
	}
	if cfg.BaselinePath() != "custom-baseline.yaml" {
		t.Errorf("BaselinePath: got %q want custom-baseline.yaml", cfg.BaselinePath())
	}
	if cfg.BaselineStaleAction() != "fail" {
		t.Errorf("--baseline-stale-fail should override stale_action to 'fail'; got %q", cfg.BaselineStaleAction())
	}
	if cfg.FixGitHistory() != "none" {
		t.Errorf("FixGitHistory override: got %q want none", cfg.FixGitHistory())
	}
}

func TestApplyCLIOverrides_StagedOnlyAndSinceMutex(t *testing.T) {
	cfg, err := config.Load(".llmlint.yaml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.ApplyCLIOverrides(config.CLIOverrides{StagedOnly: true, Since: "HEAD~1"})
	if err == nil {
		t.Error("expected error when --staged-only and --since are both set")
	}
}

func TestApplyCLIOverrides_StagedOnly(t *testing.T) {
	cfg, err := config.Load(".llmlint.yaml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ApplyCLIOverrides(config.CLIOverrides{StagedOnly: true}); err != nil {
		t.Fatal(err)
	}
	if !cfg.StagedOnly() {
		t.Error("StagedOnly should be true after CLI override")
	}
}

func TestBaselineConfig_FileOverridesAndIncludeSnippetsFalse(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
baseline:
  path: custom/path.yaml
  stale_action: ignore
  include_snippets: false
`
	if err := os.WriteFile(filepath.Join(dir, ".llmlint.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(".llmlint.yaml", dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaselinePath() != "custom/path.yaml" {
		t.Errorf("BaselinePath: got %q want custom/path.yaml", cfg.BaselinePath())
	}
	if cfg.BaselineStaleAction() != "ignore" {
		t.Errorf("stale_action: got %q want ignore", cfg.BaselineStaleAction())
	}
	if cfg.BaselineIncludeSnippets() {
		t.Error("include_snippets=false should disable snippets")
	}
}

func TestLoad_RejectsInvalidStaleAction(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
baseline:
  stale_action: explode
`
	if err := os.WriteFile(filepath.Join(dir, ".llmlint.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(".llmlint.yaml", dir); err == nil {
		t.Error("expected error for invalid stale_action; got nil")
	}
}

func TestLoad_RejectsInvalidFixGitHistory(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
fix:
  git_history: everything
`
	if err := os.WriteFile(filepath.Join(dir, ".llmlint.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(".llmlint.yaml", dir); err == nil {
		t.Error("expected error for invalid fix.git_history; got nil")
	}
}

func TestLoad_RejectsInvalidFailOn(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
fail_on: noisy
`
	if err := os.WriteFile(filepath.Join(dir, ".llmlint.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(".llmlint.yaml", dir); err == nil {
		t.Error("expected error for invalid fail_on; got nil")
	}
}

func TestLoad_RejectsInvalidRuleSeverity(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
rules:
  LLM013:
    severity: noisy
`
	if err := os.WriteFile(filepath.Join(dir, ".llmlint.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(".llmlint.yaml", dir); err == nil {
		t.Error("expected error for invalid rule severity; got nil")
	}
}

func TestLoad_RejectsUnknownRuleID(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
rules:
  LLM999:
    enabled: false
`
	if err := os.WriteFile(filepath.Join(dir, ".llmlint.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(".llmlint.yaml", dir); err == nil {
		t.Error("expected error for unknown rule id; got nil")
	}
}

func TestLoad_RejectsInvalidCategory(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
categories:
  - bogus
`
	if err := os.WriteFile(filepath.Join(dir, ".llmlint.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(".llmlint.yaml", dir); err == nil {
		t.Error("expected error for invalid category; got nil")
	}
}

func TestApplyCLIOverrides_RejectsUnknownRuleIDs(t *testing.T) {
	cfg, err := config.Load(".llmlint.yaml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ApplyCLIOverrides(config.CLIOverrides{Includes: []string{"LLM999"}}); err == nil {
		t.Error("expected error for unknown --include rule id; got nil")
	}
	if err := cfg.ApplyCLIOverrides(config.CLIOverrides{Excludes: []string{"LLM999"}}); err == nil {
		t.Error("expected error for unknown --exclude rule id; got nil")
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
