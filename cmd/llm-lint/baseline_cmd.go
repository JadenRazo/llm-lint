package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JadenRazo/llm-lint/internal/baseline"
	"github.com/JadenRazo/llm-lint/internal/config"
	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

func newBaselineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Manage .llmlint-baseline.yaml — accept findings now, gate CI on new ones",
	}
	cmd.AddCommand(newBaselineCreateCmd("create"))
	cmd.AddCommand(newBaselineCreateCmd("update"))
	cmd.AddCommand(newBaselineStatusCmd())
	cmd.AddCommand(newBaselinePruneCmd())
	return cmd
}

// baselineScanFlags adds the flags every baseline subcommand reuses.
func baselineScanFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("config", ".llmlint.yaml", "config file path (relative to repo root)")
	f.String("baseline", "", "baseline file path (default: .llmlint-baseline.yaml)")
	f.Bool("no-git", false, "skip git history scan")
	f.String("since", "", "only scan commits since this git ref/sha")
	f.StringSlice("include", nil, "force-enable rule IDs (repeatable)")
	f.StringSlice("exclude", nil, "disable rule IDs (repeatable)")
	f.String("path", ".", "repo root to scan (default: current dir)")
}

func newBaselineCreateCmd(name string) *cobra.Command {
	short := "Create a new baseline file from current findings"
	if name == "update" {
		short = "Re-create the baseline file (alias for `create --force`)"
	}
	cmd := &cobra.Command{
		Use:   name,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			force, _ := cmd.Flags().GetBool("force")
			if name == "update" {
				force = true
			}
			return runBaselineCreate(cmd, force)
		},
	}
	baselineScanFlags(cmd)
	cmd.Flags().String("output", "", "output file (default: matches --baseline / .llmlint-baseline.yaml)")
	if name == "create" {
		cmd.Flags().Bool("force", false, "overwrite existing baseline file")
	}
	return cmd
}

func newBaselineStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print stats: matched, new, and stale findings against the current baseline",
		RunE:  runBaselineStatus,
	}
	baselineScanFlags(cmd)
	return cmd
}

func newBaselinePruneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Re-write the baseline file with stale entries removed",
		RunE:  runBaselinePrune,
	}
	baselineScanFlags(cmd)
	cmd.Flags().String("output", "", "output file (default: matches --baseline)")
	return cmd
}

// loadResult performs a scan with the same flag plumbing as `scan`. Returns
// the result, the resolved baseline path, and the config (so callers can
// honor BaselineIncludeSnippets etc).
func loadResult(cmd *cobra.Command) (*engine.Result, string, *config.Config, error) {
	path, _ := cmd.Flags().GetString("path")
	if path == "" {
		path = "."
	}
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath, path)
	if err != nil {
		return nil, "", nil, err
	}
	include, _ := cmd.Flags().GetStringSlice("include")
	exclude, _ := cmd.Flags().GetStringSlice("exclude")
	noGit, _ := cmd.Flags().GetBool("no-git")
	since, _ := cmd.Flags().GetString("since")
	baselineFlag, _ := cmd.Flags().GetString("baseline")
	if err := cfg.ApplyCLIOverrides(config.CLIOverrides{
		Includes:     include,
		Excludes:     exclude,
		NoGit:        noGit,
		Since:        since,
		BaselinePath: baselineFlag,
		// Don't auto-load the baseline during baseline subcommands —
		// they want the raw current findings, not the post-baseline view.
		NoBaseline: true,
	}); err != nil {
		return nil, "", nil, err
	}

	res, err := engine.New(rules.DefaultRegistry(), cfg).Run(path)
	if err != nil {
		return nil, "", nil, err
	}
	bp := baseline.ResolvePath(cfg.BaselinePath(), path)
	return res, bp, cfg, nil
}

func runBaselineCreate(cmd *cobra.Command, force bool) error {
	res, bp, cfg, err := loadResult(cmd)
	if err != nil {
		return err
	}
	out, _ := cmd.Flags().GetString("output")
	if out != "" {
		bp = out
	}
	if !force {
		if _, err := os.Stat(bp); err == nil {
			return fmt.Errorf("baseline %s already exists; pass --force to overwrite", bp)
		}
	}

	doc := baseline.New("llm-lint v" + version)
	doc.Entries = baseline.BuildEntries(res.Findings, baseline.BuildOptions{
		IncludeSnippets: cfg.BaselineIncludeSnippets(),
	})
	if err := baseline.Save(doc, bp); err != nil {
		return fmt.Errorf("save baseline: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s with %d entries\n", bp, len(doc.Entries))
	return nil
}

func runBaselineStatus(cmd *cobra.Command, _ []string) error {
	res, bp, _, err := loadResult(cmd)
	if err != nil {
		return err
	}
	doc, err := baseline.Load(bp)
	if err != nil {
		return err
	}

	var b strings.Builder
	if doc == nil {
		fmt.Fprintf(&b, "no baseline file at %s; run `llm-lint baseline create` to create one\n", bp)
		_, _ = io.WriteString(cmd.OutOrStdout(), b.String())
		return nil
	}

	stats := baseline.Apply(res.Findings, doc)
	newCount := 0
	for _, f := range res.Findings {
		if !f.Baselined {
			newCount++
		}
	}

	fmt.Fprintf(&b, "baseline: %s (%d entries, written %s by %s)\n\n",
		bp, doc.Total, doc.GeneratedAt, doc.GeneratedBy)
	fmt.Fprintf(&b, "  matched   %d findings already baselined\n", stats.Matched)
	fmt.Fprintf(&b, "  new       %d findings not in baseline\n", newCount)
	fmt.Fprintf(&b, "  stale     %d baseline entries no longer match\n", len(stats.Stale))
	if newCount > 0 || len(stats.Stale) > 0 {
		fmt.Fprintln(&b, "\nnext:")
		if newCount > 0 {
			fmt.Fprintln(&b, "  llm-lint baseline update  # re-snapshot to absorb new findings")
		}
		if len(stats.Stale) > 0 {
			fmt.Fprintln(&b, "  llm-lint baseline prune   # drop stale entries")
		}
	}
	_, _ = io.WriteString(cmd.OutOrStdout(), b.String())
	return nil
}

func runBaselinePrune(cmd *cobra.Command, _ []string) error {
	res, bp, cfg, err := loadResult(cmd)
	if err != nil {
		return err
	}
	doc, err := baseline.Load(bp)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("no baseline file at %s", bp)
	}

	stats := baseline.Apply(res.Findings, doc)
	if len(stats.Stale) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no stale entries; baseline left unchanged")
		return nil
	}

	staleSet := map[string]bool{}
	for _, e := range stats.Stale {
		staleSet[e.Fingerprint] = true
	}
	kept := make([]baseline.Entry, 0, len(doc.Entries)-len(stats.Stale))
	for _, e := range doc.Entries {
		if !staleSet[e.Fingerprint] {
			kept = append(kept, e)
		}
	}
	doc.Entries = kept
	doc.GeneratedBy = "llm-lint v" + version
	out, _ := cmd.Flags().GetString("output")
	if out != "" {
		bp = out
	}
	if err := baseline.Save(doc, bp); err != nil {
		return fmt.Errorf("save baseline: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "pruned %d stale entries from %s; %d remain\n",
		len(stats.Stale), bp, len(kept))
	_ = cfg
	return nil
}
