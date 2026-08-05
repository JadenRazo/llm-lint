package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/JadenRazo/llm-lint/internal/config"
	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/progress"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

// sharedScanFlags registers the flags common to `scan` and every `baseline`
// subcommand. Command-specific flags (e.g. --format, --fix, --path) are
// registered by the individual commands on top of these.
func sharedScanFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("config", ".llmlint.yaml", "config file path (relative to repo root)")
	f.String("baseline", "", "baseline file path (default: .llmlint-baseline.yaml if present)")
	f.Bool("no-git", false, "skip git history scan")
	f.Bool("no-progress", false, "disable the live progress line on stderr")
	f.String("since", "", "only scan commits since this git ref/sha")
	f.StringSlice("include", nil, "force-enable rule IDs (repeatable)")
	f.StringSlice("exclude", nil, "disable rule IDs (repeatable)")
}

// scanSetup bundles everything a scanning command needs once flags are
// resolved: the effective config and an engine with progress wired up.
type scanSetup struct {
	cfg *config.Config
	eng *engine.Engine
}

// newScanSetup loads the config file, layers the shared CLI flags (and any
// command-specific overrides passed in extra) on top of it, and constructs
// the engine with a progress reporter attached. Both `scan` and the
// `baseline` subcommands go through here so flag semantics cannot drift.
func newScanSetup(cmd *cobra.Command, path string, extra config.CLIOverrides) (*scanSetup, error) {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath, path)
	if err != nil {
		return nil, err
	}

	o := extra
	o.Includes, _ = cmd.Flags().GetStringSlice("include")
	o.Excludes, _ = cmd.Flags().GetStringSlice("exclude")
	o.NoGit, _ = cmd.Flags().GetBool("no-git")
	o.Since, _ = cmd.Flags().GetString("since")
	o.BaselinePath, _ = cmd.Flags().GetString("baseline")
	if err := cfg.ApplyCLIOverrides(o); err != nil {
		return nil, err
	}

	noProgress, _ := cmd.Flags().GetBool("no-progress")
	prog := progress.New(os.Stderr, !noProgress)
	eng := engine.New(rules.DefaultRegistry(), cfg).WithProgress(prog)
	return &scanSetup{cfg: cfg, eng: eng}, nil
}
