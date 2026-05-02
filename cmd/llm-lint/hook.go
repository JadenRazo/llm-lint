package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JadenRazo/llm-lint/internal/hook"
)

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Install, uninstall, or inspect the llm-lint pre-commit hook",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHookStatus(cmd)
		},
	}
	cmd.AddCommand(newHookInstallCmd())
	cmd.AddCommand(newHookUninstallCmd())
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Print the current hook installation state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHookStatus(cmd)
		},
	})
	return cmd
}

func newHookInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install [path]",
		Short: "Install the llm-lint pre-commit hook (autodetects native vs framework)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			mode, _ := cmd.Flags().GetString("type")
			force, _ := cmd.Flags().GetBool("force")
			rev, _ := cmd.Flags().GetString("rev")
			allowMoving, _ := cmd.Flags().GetBool("allow-moving-rev")

			st, err := hook.Install(hook.InstallOptions{
				RepoRoot:       path,
				Mode:           hook.Mode(mode),
				Force:          force,
				Rev:            rev,
				BinaryVersion:  version,
				AllowMovingRev: allowMoving,
			})
			printStatus(cmd, st)
			return err
		},
	}
	f := cmd.Flags()
	f.String("type", "auto", "hook flavor: auto|native|framework")
	f.Bool("force", false, "overwrite an existing foreign pre-commit hook (native mode)")
	f.String("rev", "", "framework rev: pin to this tag (default: derived from binary version)")
	f.Bool("allow-moving-rev", false, "allow --rev HEAD/main/master/develop (NOT recommended)")
	return cmd
}

func newHookUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall [path]",
		Short: "Remove the llm-lint pre-commit hook (idempotent)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			mode, _ := cmd.Flags().GetString("type")
			st, err := hook.Uninstall(path, hook.Mode(mode))
			printStatus(cmd, st)
			return err
		},
	}
	cmd.Flags().String("type", "auto", "which hook to remove: auto|native|framework (auto fails if both are installed)")
	return cmd
}

func runHookStatus(cmd *cobra.Command) error {
	args := cmd.Flags().Args()
	path := "."
	if len(args) == 1 {
		path = args[0]
	}
	st, err := hook.GetStatus(path)
	printStatus(cmd, st)
	return err
}

func printStatus(cmd *cobra.Command, st hook.Status) {
	var b strings.Builder
	fmt.Fprintf(&b, "hook status: %s\n", st.State)
	if st.NativePath != "" {
		fmt.Fprintf(&b, "  native:    %s\n", st.NativePath)
	}
	if st.FrameworkPath != "" {
		rev := st.FrameworkRev
		if rev == "" {
			rev = "(unknown)"
		}
		fmt.Fprintf(&b, "  framework: %s (rev %s)\n", st.FrameworkPath, rev)
	}
	if st.PreCommitOnPath {
		b.WriteString("  pre-commit framework: detected on PATH\n")
	} else {
		b.WriteString("  pre-commit framework: not detected on PATH\n")
	}
	for _, n := range st.Notes {
		fmt.Fprintf(&b, "  note: %s\n", n)
	}
	_, _ = io.WriteString(cmd.OutOrStdout(), b.String())
}
