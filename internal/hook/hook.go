// Package hook installs, removes, and inspects llm-lint's pre-commit hook.
// Two flavors:
//
//   - native: a managed shell script at .git/hooks/pre-commit, marked with
//     `# llm-lint-hook-managed:start/end` so we can find and remove it
//     idempotently.
//   - framework: an entry in .pre-commit-config.yaml that the
//     pre-commit framework manages.
//
// Install autodetects which mode to use based on whether
// .pre-commit-config.yaml is already present at the repo root.
package hook

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	// MarkerStart is the literal start-of-managed-block marker in the
	// native hook script. Detection and removal are anchored to this
	// exact string — never reuse it elsewhere.
	MarkerStart = "# llm-lint-hook-managed:start"
	MarkerEnd   = "# llm-lint-hook-managed:end"

	// RepoURL is the canonical pre-commit-framework repo URL for this tool.
	RepoURL = "https://github.com/JadenRazo/llm-lint"

	// ConfigFile is the pre-commit-framework config file we edit.
	ConfigFile = ".pre-commit-config.yaml"
)

// Mode selects which hook flavor to install. Auto picks framework if a
// .pre-commit-config.yaml exists, otherwise native.
type Mode string

const (
	ModeAuto      Mode = "auto"
	ModeNative    Mode = "native"
	ModeFramework Mode = "framework"
)

// State is the post-op (or current) installed state.
type State string

const (
	StateNotInstalled State = "not-installed"
	StateNative       State = "native"
	StateFramework    State = "framework"
	StateForeign      State = "foreign"
	StateBoth         State = "both"
)

// Status is what install/uninstall/status all return. It always reflects
// post-op state; callers print it.
type Status struct {
	State           State
	NativePath      string
	FrameworkPath   string
	FrameworkRev    string
	PreCommitOnPath bool
	Notes           []string
}

// InstallOptions controls Install.
type InstallOptions struct {
	RepoRoot       string
	Mode           Mode
	Force          bool
	Rev            string // framework only; empty = derive from BinaryVersion
	BinaryVersion  string
	AllowMovingRev bool
}

// Install puts an llm-lint hook in place. Idempotent: re-running on an
// already-installed hook is a no-op (returns Status with a Note).
func Install(opts InstallOptions) (Status, error) {
	if opts.RepoRoot == "" {
		return Status{}, errors.New("repo root required")
	}
	gitDir := filepath.Join(opts.RepoRoot, ".git")
	if st, err := os.Stat(gitDir); err != nil {
		return Status{}, fmt.Errorf("not a git repository: %s", opts.RepoRoot)
	} else if !st.IsDir() {
		// .git can be a file pointing at a worktree gitdir; resolve it.
		if _, err := os.Stat(filepath.Join(opts.RepoRoot, ".git", "HEAD")); err != nil {
			return Status{}, fmt.Errorf("not a git repository: %s", opts.RepoRoot)
		}
	}

	mode := opts.Mode
	if mode == "" || mode == ModeAuto {
		mode = autodetectMode(opts.RepoRoot)
	}

	if mode == ModeNative && runtime.GOOS == "windows" {
		return Status{}, errors.New("native shell hooks not supported on Windows; use --type framework")
	}

	switch mode {
	case ModeNative:
		return installNative(opts)
	case ModeFramework:
		return installFramework(opts)
	default:
		return Status{}, fmt.Errorf("unknown hook mode: %q", mode)
	}
}

// Uninstall removes whatever managed hook is present. Never errors when
// nothing is installed.
func Uninstall(repoRoot string, mode Mode) (Status, error) {
	if repoRoot == "" {
		return Status{}, errors.New("repo root required")
	}

	cur, err := GetStatus(repoRoot)
	if err != nil {
		return cur, err
	}

	if cur.State == StateBoth && (mode == "" || mode == ModeAuto) {
		return cur, errors.New("both native and framework hooks are installed; pass --type native or --type framework")
	}

	if mode == "" || mode == ModeAuto {
		switch cur.State {
		case StateNative:
			mode = ModeNative
		case StateFramework:
			mode = ModeFramework
		default:
			cur.Notes = append(cur.Notes, "nothing to uninstall")
			return cur, nil
		}
	}

	switch mode {
	case ModeNative:
		if err := uninstallNative(repoRoot); err != nil {
			return cur, err
		}
	case ModeFramework:
		if err := uninstallFramework(repoRoot); err != nil {
			return cur, err
		}
	default:
		return cur, fmt.Errorf("unknown hook mode: %q", mode)
	}

	return GetStatus(repoRoot)
}

// GetStatus inspects the repo and returns the current hook state.
func GetStatus(repoRoot string) (Status, error) {
	st := Status{
		PreCommitOnPath: hasPreCommitFramework(),
	}

	native, foreign, err := inspectNative(repoRoot)
	if err != nil {
		return st, err
	}
	if native {
		st.NativePath = filepath.Join(repoRoot, ".git", "hooks", "pre-commit")
	}

	framework, rev, err := inspectFramework(repoRoot)
	if err != nil {
		return st, err
	}
	if framework {
		st.FrameworkPath = filepath.Join(repoRoot, ConfigFile)
		st.FrameworkRev = rev
	}

	switch {
	case native && framework:
		st.State = StateBoth
		st.Notes = append(st.Notes, "duplicate hook installation; uninstall one with --type native or --type framework")
	case native:
		st.State = StateNative
	case framework:
		st.State = StateFramework
	case foreign:
		st.State = StateForeign
	default:
		st.State = StateNotInstalled
	}
	return st, nil
}

// autodetectMode returns ModeFramework if .pre-commit-config.yaml is present
// at repo root, otherwise ModeNative (or framework on Windows).
func autodetectMode(repoRoot string) Mode {
	if runtime.GOOS == "windows" {
		return ModeFramework
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ConfigFile)); err == nil {
		return ModeFramework
	}
	return ModeNative
}

// hasPreCommitFramework reports whether `pre-commit` is on PATH.
func hasPreCommitFramework() bool {
	_, err := exec.LookPath("pre-commit")
	return err == nil
}

// resolveRev picks a rev string for framework install. Order: explicit
// --rev > derived from binary version > "v0.0.0" with a Note.
func resolveRev(opts InstallOptions) (rev string, note string, err error) {
	if opts.Rev != "" {
		if !opts.AllowMovingRev {
			switch opts.Rev {
			case "HEAD", "main", "master", "develop":
				return "", "", fmt.Errorf("--rev %q is a moving ref; pass --allow-moving-rev to override (NOT recommended)", opts.Rev)
			}
		}
		return opts.Rev, "", nil
	}
	v := opts.BinaryVersion
	if v == "" || v == "dev" {
		return "v0.0.0", "warning: pinning rev to v0.0.0 because this binary is a dev build; pass --rev <tag> for production", nil
	}
	if v[0] != 'v' {
		v = "v" + v
	}
	return v, "", nil
}
