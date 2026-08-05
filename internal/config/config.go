package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"sigs.k8s.io/yaml"

	"github.com/JadenRazo/llm-lint/internal/rules"
)

type RuleOverride struct {
	Enabled  *bool          `json:"enabled,omitempty"`
	Severity rules.Severity `json:"severity,omitempty"`
}

type ScanConfig struct {
	Filesystem      *bool `json:"filesystem,omitempty"`
	GitHistory      *bool `json:"git_history,omitempty"`
	GitHistoryDepth int   `json:"git_history_depth,omitempty"`
}

type BaselineConfig struct {
	Path            string `json:"path,omitempty"`
	StaleAction     string `json:"stale_action,omitempty"` // warn | fail | ignore
	IncludeSnippets *bool  `json:"include_snippets,omitempty"`
}

type FixConfig struct {
	// GitHistory controls whether --fix rewrites commit messages.
	// Values: none | latest | scanned. "latest" is the conservative default.
	GitHistory string `json:"git_history,omitempty"`
}

type Config struct {
	Version    int                     `json:"version,omitempty"`
	Categories []rules.Category        `json:"categories,omitempty"`
	Rules      map[string]RuleOverride `json:"rules,omitempty"`
	Ignore     []string                `json:"ignore,omitempty"`
	Scan       ScanConfig              `json:"scan,omitempty"`
	Baseline   BaselineConfig          `json:"baseline,omitempty"`
	Fix        FixConfig               `json:"fix,omitempty"`
	FailOn     rules.Severity          `json:"fail_on,omitempty"`

	includeRules      map[string]bool
	excludeRules      map[string]bool
	noGit             bool
	since             string
	stagedOnly        bool
	baselinePath      string
	noBaseline        bool
	baselineStaleFail bool
	fixGitHistory     string
}

func defaultConfig() *Config {
	return &Config{
		Version: 1,
		Ignore: []string{
			"vendor/**",
			"node_modules/**",
			"**/*.min.js",
			"**/*.min.css",
		},
		Scan: ScanConfig{
			GitHistoryDepth: 1000,
		},
		FailOn:       rules.SevError,
		includeRules: map[string]bool{},
		excludeRules: map[string]bool{},
	}
}

func Load(configPath, root string) (*Config, error) {
	cfg := defaultConfig()

	if configPath == "" {
		configPath = ".llmlint.yaml"
	}
	full := configPath
	if !filepath.IsAbs(configPath) {
		full = filepath.Join(root, configPath)
	}

	data, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read %s: %w", full, err)
	}
	if err := yaml.UnmarshalStrict(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", full, err)
	}
	if err := cfg.validate(full); err != nil {
		return nil, err
	}
	if cfg.Rules == nil {
		cfg.Rules = map[string]RuleOverride{}
	}
	if cfg.includeRules == nil {
		cfg.includeRules = map[string]bool{}
	}
	if cfg.excludeRules == nil {
		cfg.excludeRules = map[string]bool{}
	}
	if cfg.FailOn == "" {
		cfg.FailOn = rules.SevError
	}
	if cfg.Scan.GitHistoryDepth == 0 {
		cfg.Scan.GitHistoryDepth = 1000
	}
	if cfg.Fix.GitHistory == "" {
		cfg.Fix.GitHistory = "latest"
	}
	if err := validateFixGitHistory(cfg.Fix.GitHistory); err != nil {
		return nil, err
	}
	if cfg.Baseline.StaleAction != "" {
		switch cfg.Baseline.StaleAction {
		case "warn", "fail", "ignore":
		default:
			return nil, fmt.Errorf("invalid baseline.stale_action %q (want warn|fail|ignore)", cfg.Baseline.StaleAction)
		}
	}
	return cfg, nil
}

// validate rejects config values that would otherwise fail silently: a
// typo'd rule ID re-enables a rule the team meant to disable, an unknown
// category disables every rule, and an unknown severity drops findings out
// of every summary bucket. All of these are silent false negatives, so they
// are load errors, not warnings.
func (c *Config) validate(path string) error {
	if c.Version != 0 && c.Version != 1 {
		return fmt.Errorf("%s: unsupported config version %d (this build supports version 1)", path, c.Version)
	}
	for _, cat := range c.Categories {
		if !rules.ValidCategory(cat) {
			return fmt.Errorf("%s: unknown category %q (known: %v)", path, cat, rules.AllCategories())
		}
	}
	for id, ov := range c.Rules {
		if _, ok := rules.Get(id); !ok {
			return fmt.Errorf("%s: unknown rule id %q in rules section", path, id)
		}
		if ov.Severity != "" && !ov.Severity.Valid() {
			return fmt.Errorf("%s: invalid severity %q for rule %s (want error|warning|info)", path, ov.Severity, id)
		}
	}
	if c.FailOn != "" && !c.FailOn.Valid() {
		return fmt.Errorf("%s: invalid fail_on %q (want error|warning|info)", path, c.FailOn)
	}
	for _, pat := range c.Ignore {
		if !doublestar.ValidatePattern(pat) {
			return fmt.Errorf("%s: invalid ignore glob %q", path, pat)
		}
	}
	return nil
}

// CLIOverrides bundles per-invocation flags that override values from the
// config file. Future flags add new fields here so we don't churn the
// ApplyCLIOverrides signature on every CLI addition.
type CLIOverrides struct {
	Includes          []string
	Excludes          []string
	NoGit             bool
	Since             string
	StagedOnly        bool
	BaselinePath      string
	NoBaseline        bool
	BaselineStaleFail bool
	FixGitHistory     string
}

func (c *Config) ApplyCLIOverrides(o CLIOverrides) error {
	if o.StagedOnly && o.Since != "" {
		return errors.New("--staged-only and --since are mutually exclusive")
	}
	for _, id := range o.Includes {
		if id == "" {
			continue
		}
		if _, ok := rules.Get(id); !ok {
			return fmt.Errorf("unknown rule id %q in --include", id)
		}
		c.includeRules[id] = true
	}
	for _, id := range o.Excludes {
		if id == "" {
			continue
		}
		if _, ok := rules.Get(id); !ok {
			return fmt.Errorf("unknown rule id %q in --exclude", id)
		}
		c.excludeRules[id] = true
	}
	c.noGit = o.NoGit
	c.since = o.Since
	c.stagedOnly = o.StagedOnly
	c.baselinePath = o.BaselinePath
	c.noBaseline = o.NoBaseline
	c.baselineStaleFail = o.BaselineStaleFail
	if o.FixGitHistory != "" {
		if err := validateFixGitHistory(o.FixGitHistory); err != nil {
			return err
		}
		c.fixGitHistory = o.FixGitHistory
	}
	return nil
}

func (c *Config) Since() string { return c.since }

func (c *Config) StagedOnly() bool { return c.stagedOnly }

func (c *Config) BaselineEnabled() bool { return !c.noBaseline }

func (c *Config) BaselinePath() string {
	if c.baselinePath != "" {
		return c.baselinePath
	}
	return c.Baseline.Path
}

func (c *Config) BaselineStaleAction() string {
	if c.baselineStaleFail {
		return "fail"
	}
	if c.Baseline.StaleAction != "" {
		return c.Baseline.StaleAction
	}
	return "warn"
}

func (c *Config) BaselineIncludeSnippets() bool {
	if c.Baseline.IncludeSnippets != nil {
		return *c.Baseline.IncludeSnippets
	}
	return true
}

func (c *Config) FixGitHistory() string {
	if c.fixGitHistory != "" {
		return c.fixGitHistory
	}
	if c.Fix.GitHistory != "" {
		return c.Fix.GitHistory
	}
	return "latest"
}

func (c *Config) GitEnabled() bool {
	if c.noGit {
		return false
	}
	if c.Scan.GitHistory != nil {
		return *c.Scan.GitHistory
	}
	return true
}

func (c *Config) FilesystemEnabled() bool {
	if c.Scan.Filesystem != nil {
		return *c.Scan.Filesystem
	}
	return true
}

func (c *Config) IsRuleEnabled(id string) bool {
	if c.excludeRules[id] {
		return false
	}
	if c.includeRules[id] {
		return true
	}
	if ov, ok := c.Rules[id]; ok && ov.Enabled != nil && !*ov.Enabled {
		return false
	}
	if len(c.Categories) > 0 {
		r, ok := rules.Get(id)
		if !ok {
			return true
		}
		for _, cat := range c.Categories {
			if cat == r.Category {
				return true
			}
		}
		return false
	}
	return true
}

func (c *Config) EffectiveSeverity(id string, def rules.Severity) rules.Severity {
	if ov, ok := c.Rules[id]; ok && ov.Severity != "" {
		return ov.Severity
	}
	return def
}

// IsIgnoredDir reports whether an entire directory can be pruned from the
// walk. A pattern like "vendor/**" means "everything under vendor/", so the
// directory "vendor" itself is prunable even though the pattern does not
// match the bare string "vendor" (or "vendor/"). Without this, default
// ignores like node_modules/** were filtered file-by-file after walking the
// whole subtree.
func (c *Config) IsIgnoredDir(relDir string) bool {
	relDir = strings.TrimSuffix(relDir, "/")
	if c.IsIgnored(relDir) || c.IsIgnored(relDir+"/") {
		return true
	}
	for _, pat := range c.Ignore {
		base, found := strings.CutSuffix(pat, "/**")
		if !found {
			continue
		}
		if ok, _ := doublestar.Match(base, relDir); ok {
			return true
		}
	}
	return false
}

func (c *Config) IsIgnored(relPath string) bool {
	for _, pat := range c.Ignore {
		// Match (not PathMatch): callers pass slash-normalized paths, and
		// PathMatch would split on the OS separator, breaking Windows.
		// Patterns are validated at load time, so the error is impossible.
		if ok, _ := doublestar.Match(pat, relPath); ok {
			return true
		}
	}
	return false
}

func (c *Config) HistoryDepth() int { return c.Scan.GitHistoryDepth }

func validateFixGitHistory(s string) error {
	switch s {
	case "none", "latest", "scanned":
		return nil
	default:
		return fmt.Errorf("invalid fix.git_history %q (want none|latest|scanned)", s)
	}
}
