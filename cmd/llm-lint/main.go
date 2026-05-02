package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JadenRazo/llm-lint/internal/config"
	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/progress"
	"github.com/JadenRazo/llm-lint/internal/report"
	"github.com/JadenRazo/llm-lint/internal/rules"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

var version = "dev"

func main() {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "llm-lint",
		Short:         "Catch LLM artifacts (CLAUDE.md, Co-authored-by trailers, .cursorrules, etc.) in your codebase.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newScanCmd())
	root.AddCommand(newRulesCmd())
	root.AddCommand(newHookCmd())
	root.AddCommand(newBaselineCmd())
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run:   func(_ *cobra.Command, _ []string) { fmt.Println(version) },
	})
	return root
}

func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Scan a repository for LLM artifacts",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runScan,
	}
	f := cmd.Flags()
	f.String("config", ".llmlint.yaml", "config file path (relative to repo root)")
	f.String("format", "human", "output format: human|json|sarif|github (auto-detects to github when GITHUB_ACTIONS=true)")
	f.String("output", "-", "output file or '-' for stdout")
	f.String("fail-on", "error", "exit non-zero if any finding is at or above this severity (error|warning|info|none)")
	f.Bool("no-git", false, "skip git history scan")
	f.Bool("no-color", false, "disable ANSI color")
	f.Bool("no-progress", false, "disable the live progress line on stderr")
	f.String("since", "", "only scan commits since this git ref/sha")
	f.Bool("staged-only", false, "scan files staged in the git index instead of the working tree (skips trailer/message rules)")
	f.String("baseline", "", "baseline file path (default: .llmlint-baseline.yaml if present)")
	f.Bool("no-baseline", false, "ignore baseline file even if present")
	f.Bool("baseline-stale-fail", false, "exit non-zero if the baseline has stale entries")
	f.StringSlice("include", nil, "force-enable rule IDs (repeatable)")
	f.StringSlice("exclude", nil, "disable rule IDs (repeatable)")
	f.Bool("pr-comment", false, "post a sticky PR comment with findings (requires --format github and GITHUB_TOKEN)")
	f.String("pr-comment-mode", "sticky", "PR comment mode: sticky|append")
	f.String("gh-token", "", "GitHub token (overrides GITHUB_TOKEN; never logged)")
	f.String("gh-repo", "", "owner/repo (overrides GITHUB_REPOSITORY)")
	f.Int("gh-pr", 0, "PR number (overrides auto-detect from event)")
	f.BoolP("verbose", "v", false, "verbose output")
	return cmd
}

func runScan(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) == 1 {
		path = args[0]
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath, path)
	if err != nil {
		return err
	}
	include, _ := cmd.Flags().GetStringSlice("include")
	exclude, _ := cmd.Flags().GetStringSlice("exclude")
	noGit, _ := cmd.Flags().GetBool("no-git")
	since, _ := cmd.Flags().GetString("since")
	stagedOnly, _ := cmd.Flags().GetBool("staged-only")
	baselinePath, _ := cmd.Flags().GetString("baseline")
	noBaseline, _ := cmd.Flags().GetBool("no-baseline")
	baselineStaleFail, _ := cmd.Flags().GetBool("baseline-stale-fail")
	if err := cfg.ApplyCLIOverrides(config.CLIOverrides{
		Includes:          include,
		Excludes:          exclude,
		NoGit:             noGit,
		Since:             since,
		StagedOnly:        stagedOnly,
		BaselinePath:      baselinePath,
		NoBaseline:        noBaseline,
		BaselineStaleFail: baselineStaleFail,
	}); err != nil {
		return err
	}

	noProgress, _ := cmd.Flags().GetBool("no-progress")
	prog := progress.New(os.Stderr, !noProgress)

	eng := engine.New(rules.DefaultRegistry(), cfg).WithProgress(prog)
	res, err := eng.Run(path)
	if err != nil {
		return err
	}

	format, _ := cmd.Flags().GetString("format")
	formatChanged := cmd.Flags().Changed("format")
	format = report.AutoDetectFormat(os.Getenv, formatChanged, format)

	output, _ := cmd.Flags().GetString("output")
	noColor, _ := cmd.Flags().GetBool("no-color")
	prComment, _ := cmd.Flags().GetBool("pr-comment")
	prMode, _ := cmd.Flags().GetString("pr-comment-mode")
	ghToken, _ := cmd.Flags().GetString("gh-token")
	ghRepo, _ := cmd.Flags().GetString("gh-repo")
	ghPR, _ := cmd.Flags().GetInt("gh-pr")
	rep, err := report.New(format, report.Options{
		NoColor: noColor,
		Output:  output,
		Version: version,
		GitHub: report.GitHubOptions{
			PRComment:     prComment,
			PRCommentMode: prMode,
			Token:         ghToken,
			Repo:          ghRepo,
			PRNumber:      ghPR,
		},
	})
	if err != nil {
		return err
	}
	if err := rep.Write(res); err != nil {
		return err
	}

	failOn, _ := cmd.Flags().GetString("fail-on")
	if engine.ExceedsThreshold(res, failOn) {
		os.Exit(1)
	}
	if cfg.BaselineStaleAction() == "fail" && res.StaleBaselineCount > 0 {
		os.Exit(1)
	}
	return nil
}

func newRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "List or describe rules",
		Run: func(_ *cobra.Command, _ []string) {
			for _, r := range rules.All() {
				fmt.Printf("%s  %-7s  %-9s  %s\n", r.ID, r.Severity, r.Category, r.Title)
			}
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show <ID>",
		Short: "Show full description and remediation for a rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := strings.ToUpper(args[0])
			r, ok := rules.Get(id)
			if !ok {
				return fmt.Errorf("unknown rule: %s", id)
			}
			fmt.Printf("ID:          %s\n", r.ID)
			fmt.Printf("Title:       %s\n", r.Title)
			fmt.Printf("Severity:    %s\n", r.Severity)
			fmt.Printf("Category:    %s\n", r.Category)
			fmt.Printf("Kind:        %s\n", r.Kind)
			fmt.Printf("\nDescription:\n  %s\n", r.Description)
			fmt.Printf("\nRemediation:\n%s\n", indentText(r.Remediation, "  "))
			return nil
		},
	})
	return cmd
}

func indentText(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
