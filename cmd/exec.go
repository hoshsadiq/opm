package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tbcrawford/opm/internal/symlink"
)

var (
	execIsolate  bool
	execDataDir  bool
	execCacheDir bool
	execLogDir   bool
	execStateDir bool
)

var execCmd = &cobra.Command{
	Use:   "exec <profile> [-- command [args...]]",
	Short: "Run a command with a specific profile active",
	Long: `Run opencode (or any command) using the named profile, without changing
the global active profile. By default, the spawned process reads its OpenCode
configuration directly from the profile via OPENCODE_CONFIG_DIR.

Use --isolate to run with a persistent XDG config root under
~/.cache/opm/profiles/<name>/, with optional data/cache/log/state directories.

If no command is provided, opencode is launched.

Examples:
  opm exec work
  opm exec personal -- opencode --no-auto-update
  opm exec ci -- opencode run "fix the tests"`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: execCompletion,
	SilenceUsage:      true,
	PreRunE:           managedGuard,
	RunE:              runExec,
}

func init() {
	execCmd.Flags().BoolVar(&execIsolate, "isolate", false, "use a persistent isolated XDG config root")
	execCmd.Flags().BoolVar(&execDataDir, "data-dir", false, "create and export OPENCODE_DATA_DIR when used with --isolate")
	execCmd.Flags().BoolVar(&execCacheDir, "cache-dir", false, "create and export OPENCODE_CACHE_DIR when used with --isolate")
	execCmd.Flags().BoolVar(&execLogDir, "log-dir", false, "create and export OPENCODE_LOG_DIR when used with --isolate")
	execCmd.Flags().BoolVar(&execStateDir, "state-dir", false, "create and export OPENCODE_STATE_DIR when used with --isolate")
	markRootHelpGroup(execCmd, helpGroupProfiles)
	markRootHelpOrder(execCmd, 35)
	rootCmd.AddCommand(execCmd)
}

// execCompletion completes the first argument (profile name) and offers no
// completions for subsequent arguments (those are passed to the child command).
func execCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		// After the profile name, defer to the shell's own completion.
		return nil, cobra.ShellCompDirectiveDefault
	}
	return completeProfileNames(args, 1, false)
}

func runExec(cmd *cobra.Command, args []string) error {
	profileName := args[0]

	// Remaining args (after optional "--") are the command + arguments to run.
	// If none supplied, default to opencode.
	childArgs := args[1:]
	if len(childArgs) == 0 {
		childArgs = []string{"opencode"}
	}

	s := newStore()
	profileDir, err := s.GetProfile(profileName)
	if err != nil {
		return err
	}

	child := exec.Command(childArgs[0], childArgs[1:]...) //nolint:gosec
	if !execIsolate {
		child.Env = append(os.Environ(), "OPENCODE_CONFIG_DIR="+profileDir)
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("determine home directory: %w", err)
		}
		isolateDir := filepath.Join(homeDir, ".cache", "opm", "profiles", profileName)
		if err := os.MkdirAll(isolateDir, 0o755); err != nil {
			return fmt.Errorf("create isolate directory: %w", err)
		}

		opencodeLink := filepath.Join(isolateDir, "opencode")
		if err := symlink.SetAtomic(profileDir, opencodeLink); err != nil {
			return fmt.Errorf("create isolate symlink: %w", err)
		}

		child.Env = append(os.Environ(),
			"XDG_CONFIG_HOME="+isolateDir,
			"OPENCODE_CONFIG_DIR="+opencodeLink,
		)

		if execDataDir {
			dataDir := filepath.Join(isolateDir, "data")
			if err := os.MkdirAll(dataDir, 0o755); err != nil {
				return fmt.Errorf("create data directory: %w", err)
			}
			child.Env = append(child.Env, "OPENCODE_DATA_DIR="+dataDir)
		}
		if execCacheDir {
			cacheDir := filepath.Join(isolateDir, "cache")
			if err := os.MkdirAll(cacheDir, 0o755); err != nil {
				return fmt.Errorf("create cache directory: %w", err)
			}
			child.Env = append(child.Env, "OPENCODE_CACHE_DIR="+cacheDir)
		}
		if execLogDir {
			logDir := filepath.Join(isolateDir, "log")
			if err := os.MkdirAll(logDir, 0o755); err != nil {
				return fmt.Errorf("create log directory: %w", err)
			}
			child.Env = append(child.Env, "OPENCODE_LOG_DIR="+logDir)
		}
		if execStateDir {
			stateDir := filepath.Join(isolateDir, "state")
			if err := os.MkdirAll(stateDir, 0o755); err != nil {
				return fmt.Errorf("create state directory: %w", err)
			}
			child.Env = append(child.Env, "OPENCODE_STATE_DIR="+stateDir)
		}
	}
	child.Stdin = os.Stdin
	child.Stdout = cmd.OutOrStdout()
	child.Stderr = cmd.ErrOrStderr()

	if err := child.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Propagate the child's exit code without printing a redundant error.
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("exec: %w", err)
	}
	return nil
}
