package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/enr/paq/internal/state"
	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:   "exec <app[@version]>... -- <command> [args...]",
	Short: "Run a command with installed tools first on PATH",
	Long: "Run a command with the bin directories of the given installed tools first on PATH, " +
		"without changing your shell's PATH. Useful for project-local tools (`paq --local exec`), " +
		"for a specific version when several are installed, and in CI or agent sessions. " +
		"The tools must be installed: exec never downloads anything. " +
		"paq exits with the command's exit code.",
	Example: `  paq --local exec node -- npm ci
  paq exec terraform@1.9.8 -- terraform plan
  paq exec jdk maven -- mvn -v`,
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: completeInstalledApps,
	RunE:              runExec,
}

func init() {
	rootCmd.AddCommand(execCmd)
}

// childExitError carries the exit code of the command run by `paq exec`, so
// paq exits with it without reporting an error of its own.
type childExitError struct{ code int }

func (e childExitError) Error() string { return fmt.Sprintf("command exited with status %d", e.code) }

func runExec(cmd *cobra.Command, args []string) error {
	dash := cmd.ArgsLenAtDash()
	if dash < 1 || dash == len(args) {
		return usageError{"usage: paq exec <app[@version]>... -- <command> [args...]"}
	}
	apps, command := args[:dash], args[dash:]

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	var dirs []string
	for _, arg := range apps {
		rec, err := resolveExecTarget(st, arg)
		if err != nil {
			return err
		}
		dirs = append(dirs, execBinDir(rec))
	}

	// Set on paq's own environment too: exec.Command resolves the command
	// against this process's PATH.
	path := strings.Join(append(dirs, os.Getenv("PATH")), string(os.PathListSeparator))
	if err := os.Setenv("PATH", path); err != nil {
		return err
	}
	child := exec.Command(command[0], command[1:]...)
	if errors.Is(child.Err, exec.ErrNotFound) {
		return hintError{
			msg:  fmt.Sprintf("command %q not found", command[0]),
			hint: fmt.Sprintf("the tools' executables are in %s", strings.Join(dirs, ", ")),
		}
	}
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := child.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return childExitError{code: exitErr.ExitCode()}
		}
		return fmt.Errorf("run %s: %w", command[0], err)
	}
	return nil
}

// resolveExecTarget returns the installed record for an "app" or
// "app@version" argument, refusing one whose files are gone.
func resolveExecTarget(st *state.State, arg string) (state.InstalledApp, error) {
	name, version := parseAppRef(arg)
	matches := st.ByName(name)
	if version != "" {
		matches = nil
		if rec, ok := st.Get(name, version); ok {
			matches = []state.InstalledApp{rec}
		}
	}
	switch {
	case len(matches) == 0:
		return state.InstalledApp{}, hintError{
			msg:          fmt.Sprintf("%s is not installed", arg),
			hint:         fmt.Sprintf("install it with `paq install %s`", arg),
			suggestLocal: true,
		}
	case len(matches) > 1:
		var versions []string
		for _, m := range matches {
			versions = append(versions, m.Version)
		}
		return state.InstalledApp{}, hintError{
			msg:  fmt.Sprintf("multiple versions of %q installed: %s", name, strings.Join(versions, ", ")),
			hint: fmt.Sprintf("specify one with %s@<version>", name),
		}
	}
	rec := matches[0]
	if gone := rec.MissingPaths(); len(gone) > 0 {
		return state.InstalledApp{}, hintError{
			msg:  fmt.Sprintf("%s %s is recorded as installed but missing on disk: %s", rec.Name, rec.Version, strings.Join(gone, ", ")),
			hint: fmt.Sprintf("reinstall it with `paq install %s --force`", rec.Name),
		}
	}
	return rec, nil
}

// execBinDir returns the directory holding rec's executables: the folder of
// a single file, the bin dir of a multi-binary install, and the bin/
// subfolder of a directory install when it has one (node, jdk, ...).
func execBinDir(rec state.InstalledApp) string {
	switch rec.Kind {
	case "file":
		return filepath.Dir(rec.Dest)
	case "dir":
		if fi, err := os.Stat(filepath.Join(rec.Dest, "bin")); err == nil && fi.IsDir() {
			return filepath.Join(rec.Dest, "bin")
		}
	}
	return rec.Dest
}
