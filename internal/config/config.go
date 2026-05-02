package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

type Config struct {
	Version    int                     `json:"version,omitempty"`
	Categories []rules.Category        `json:"categories,omitempty"`
	Rules      map[string]RuleOverride `json:"rules,omitempty"`
	Ignore     []string                `json:"ignore,omitempty"`
	Scan       ScanConfig              `json:"scan,omitempty"`
	Baseline   BaselineConfig          `json:"baseline,omitempty"`
	FailOn     rules.Severity          `json:"fail_on,omitempty"`

	includeRules      map[string]bool
	excludeRules      map[string]bool
	noGit             bool
	since             string
	stagedOnly        bool
	baselinePath      string
	noBaseline        bool
	baselineStaleFail bool
	root              string
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
	cfg.root = root

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
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", full, err)
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
	if cfg.Baseline.StaleAction != "" {
		switch cfg.Baseline.StaleAction {
		case "warn", "fail", "ignore":
		default:
			return nil, fmt.Errorf("invalid baseline.stale_action %q (want warn|fail|ignore)", cfg.Baseline.StaleAction)
		}
	}
	return cfg, nil
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
}

func (c *Config) ApplyCLIOverrides(o CLIOverrides) error {
	if o.StagedOnly && o.Since != "" {
		return errors.New("--staged-only and --since are mutually exclusive")
	}
	for _, id := range o.Includes {
		if id != "" {
			c.includeRules[id] = true
		}
	}
	for _, id := range o.Excludes {
		if id != "" {
			c.excludeRules[id] = true
		}
	}
	c.noGit = o.NoGit
	c.since = o.Since
	c.stagedOnly = o.StagedOnly
	c.baselinePath = o.BaselinePath
	c.noBaseline = o.NoBaseline
	c.baselineStaleFail = o.BaselineStaleFail
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

func (c *Config) IsIgnored(relPath string) bool {
	for _, pat := range c.Ignore {
		if ok, _ := doublestar.PathMatch(pat, relPath); ok {
			return true
		}
	}
	return false
}

func (c *Config) HistoryDepth() int { return c.Scan.GitHistoryDepth }

func (c *Config) FailOnRank() int { return c.FailOn.Rank() }
