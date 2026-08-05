package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/JadenRazo/llm-lint/internal/config"
	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/fixer"
	"github.com/JadenRazo/llm-lint/internal/report"
	"github.com/JadenRazo/llm-lint/internal/rules"
	"github.com/JadenRazo/llm-lint/internal/textutil"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

// Build metadata, injected via -ldflags at release time (see Makefile and
// .goreleaser.yaml). Defaults describe a plain `go build`.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// versionString is the long-form version line shared by `llm-lint version`
// and `llm-lint --version`.
func versionString() string {
	return fmt.Sprintf("llm-lint %s (commit %s, built %s, %s)", version, commit, date, runtime.Version())
}

// Exit codes. Documented in README.md; keep the two lists in sync.
const (
	exitOK            = 0
	exitThreshold     = 1 // findings at or above --fail-on
	exitInternal      = 2 // config, IO, or internal error
	exitStaleBaseline = 3 // baseline has stale entries and stale_action=fail
)

// exitCodeError carries a specific process exit code up to main() so that
// command logic never calls os.Exit directly (which would skip deferred
// cleanup and make the paths untestable).
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string { return e.msg }

func main() {
	// Ctrl-C cancels the context, which stops walkers, git iteration, and
	// any in-flight git subprocess (CommandContext) instead of leaving a
	// half-rewritten history behind.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := newRoot().ExecuteContext(ctx)
	stop()
	if err == nil {
		os.Exit(exitOK)
	}
	var ec *exitCodeError
	if errors.As(err, &ec) {
		if ec.msg != "" {
			fmt.Fprintln(os.Stderr, ec.msg)
		}
		os.Exit(ec.code)
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(exitInternal)
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "llm-lint",
		Short:         "Catch LLM artifacts (CLAUDE.md, Co-authored-by trailers, .cursorrules, etc.) in your codebase.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Setting Version gives the root command a --version flag.
		Version: version,
	}
	root.SetVersionTemplate(versionString() + "\n")
	root.AddCommand(newScanCmd())
	root.AddCommand(newRulesCmd())
	root.AddCommand(newHookCmd())
	root.AddCommand(newBaselineCmd())
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version and build metadata",
		Run: func(cmd *cobra.Command, _ []string) {
			if short, _ := cmd.Flags().GetBool("short"); short {
				// Bare version string, for scripts that parse the output.
				fmt.Println(version)
				return
			}
			fmt.Println(versionString())
		},
	}
	versionCmd.Flags().Bool("short", false, "print just the version number")
	root.AddCommand(versionCmd)
	return root
}

func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Scan a repository for LLM artifacts",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runScan,
	}
	sharedScanFlags(cmd)
	f := cmd.Flags()
	f.String("format", "human", "output format: human|json|sarif|github (auto-detects to github when GITHUB_ACTIONS=true)")
	f.String("output", "-", "output file or '-' for stdout")
	f.String("fail-on", "", "exit non-zero if any finding is at or above this severity (error|warning|info|none) (default: config file's fail_on or \"error\")")
	f.Bool("no-color", false, "disable ANSI color")
	f.Bool("staged-only", false, "scan files staged in the git index instead of the working tree (skips trailer/message rules)")
	f.Bool("no-baseline", false, "ignore baseline file even if present")
	f.Bool("baseline-stale-fail", false, "exit non-zero if the baseline has stale entries")
	f.Bool("fix", false, "apply safe automatic fixes before reporting remaining findings")
	f.Bool("fix-preview", false, "preview safe automatic fixes without modifying files, index, or git history")
	f.String("fix-git-history", "", "with --fix/--fix-preview, rewrite commit messages: none|latest|scanned (default: config fix.git_history or latest)")
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

	stagedOnly, _ := cmd.Flags().GetBool("staged-only")
	noBaseline, _ := cmd.Flags().GetBool("no-baseline")
	baselineStaleFail, _ := cmd.Flags().GetBool("baseline-stale-fail")
	fix, _ := cmd.Flags().GetBool("fix")
	fixPreview, _ := cmd.Flags().GetBool("fix-preview")
	fixGitHistory, _ := cmd.Flags().GetString("fix-git-history")
	if fix && fixPreview {
		return fmt.Errorf("--fix and --fix-preview cannot be used together")
	}
	runFix := fix || fixPreview
	if runFix && stagedOnly {
		return fmt.Errorf("--fix/--fix-preview cannot be used with --staged-only because staged-only scans the git index, not the worktree")
	}
	if !runFix && fixGitHistory != "" {
		return fmt.Errorf("--fix-git-history requires --fix or --fix-preview")
	}
	setup, err := newScanSetup(cmd, path, config.CLIOverrides{
		StagedOnly:        stagedOnly,
		NoBaseline:        noBaseline,
		BaselineStaleFail: baselineStaleFail,
		FixGitHistory:     fixGitHistory,
	})
	if err != nil {
		return err
	}
	cfg := setup.cfg

	// Resolve effective --fail-on: CLI flag wins, then config-file fail_on,
	// then the default "error" baked into config.Load. Empty flag default
	// lets us tell "user didn't pass it" apart from "user passed error".
	// Validated here, before any scanning or report writing, so a bad value
	// fails fast instead of after a full (possibly slow) scan.
	failOn, _ := cmd.Flags().GetString("fail-on")
	if failOn == "" {
		failOn = string(cfg.FailOn)
	}
	if err := engine.ValidateFailOn(failOn); err != nil {
		return err
	}

	ctx := cmd.Context()
	res, err := setup.eng.RunContext(ctx, path)
	if err != nil {
		return err
	}
	if runFix {
		summary, err := fixer.ApplyWithOptions(ctx, path, res.Findings, rules.DefaultRegistry(), fixer.Options{
			GitHistoryMode: cfg.FixGitHistory(),
			Preview:        fixPreview,
		})
		if err != nil {
			return err
		}
		if err := writeFixSummary(os.Stderr, summary, fixPreview); err != nil {
			return err
		}
		if summary.Unfixable > 0 {
			if _, err := fmt.Fprintf(os.Stderr, "remaining: %d findings require manual review or history cleanup\n", summary.Unfixable); err != nil {
				return err
			}
		}
		if !fixPreview {
			res, err = setup.eng.RunContext(ctx, path)
			if err != nil {
				return err
			}
		}
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

	if engine.ExceedsThreshold(res, failOn) {
		return &exitCodeError{code: exitThreshold}
	}
	if cfg.BaselineStaleAction() == "fail" && res.StaleBaselineCount > 0 {
		return &exitCodeError{
			code: exitStaleBaseline,
			msg:  fmt.Sprintf("baseline: %d stale entries (run `llm-lint baseline prune` or rerun `baseline create`)", res.StaleBaselineCount),
		}
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
			fmt.Printf("\nRemediation:\n%s\n", textutil.Indent(r.Remediation, "  "))
			return nil
		},
	})
	return cmd
}

func writeFixSummary(w *os.File, summary fixer.Summary, preview bool) error {
	if summary.Empty() {
		return nil
	}
	verb := "fixed"
	if preview {
		verb = "would fix"
	}
	_, err := fmt.Fprintf(w, "%s: %d files changed, %d lines removed, %d commit messages cleaned, %d commit lines removed, %d .gitignore entries added, %d index entries untracked\n",
		verb, summary.FilesChanged, summary.LinesRemoved, summary.CommitMessages, summary.CommitLinesRemoved, summary.GitignoreAdded, summary.IndexEntriesFixed)
	return err
}
