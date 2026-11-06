package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/enr/paq/embedded"
	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/registry"
	"github.com/enr/paq/internal/ui"
	"github.com/enr/paq/internal/verify"
	"github.com/spf13/cobra"
)

var (
	flagInstallForce         bool
	flagInstallNoSave        bool
	flagInstallRequireVerify bool
	flagInstallDest          string
)

const maxParallel = 3

var installCmd = &cobra.Command{
	Use:     "install [app[@version]...]",
	Aliases: []string{"i"},
	Short:   "Install a tool (or all tools from manifest if no app specified)",
	Long: "Install one or more tools. A name that isn't yet in the manifest is auto-imported " +
		"from the registry, with a default version and destination, and recorded there once installed: a failed install leaves the manifest untouched. " +
		"Append @version to install that version: it is recorded as the app's version in the manifest " +
		"(only for this run with --no-save). " +
		"With no arguments, installs every app declared in the manifest instead. " +
		"Installing more than one app runs them concurrently (up to 3 at a time), " +
		"each with a [name]-prefixed line of output.",
	Example: `  paq install ripgrep            # install, recording it in the manifest
  paq install ripgrep --no-save  # install without recording it (ephemeral)
  paq install ripgrep@14.1.0     # install (and record) a specific version
  paq install ripgrep bat delta  # install multiple tools
  paq install terraform@1.9.8 --no-save --dest '/opt/paq/terraform-{{version}}/terraform'
                                 # side by side with other versions
  paq install                    # install every tool from the manifest`,
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: completeInstallableNames,
	RunE:              runInstall,
}

func init() {
	installCmd.Flags().BoolVarP(&flagInstallForce, "force", "f", false, "Reinstall even if already installed")
	installCmd.Flags().BoolVar(&flagInstallNoSave, "no-save", false, "Install without recording the tool in the manifest (ephemeral)")
	installCmd.Flags().StringVar(&flagInstallDest, "dest", "", "Install destination for a single app (a template: {{version}} etc.); recorded for a new manifest entry")
	installCmd.Flags().BoolVar(&flagInstallRequireVerify, "require-verify", false, "Fail instead of warning when a recipe has no checksum or signature (also PAQ_REQUIRE_VERIFY=1)")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	if flagInstallDest != "" && len(args) != 1 {
		return usageError{"--dest needs exactly one app"}
	}

	// A single explicit app gets the friendlier single-app UX: no [name]
	// prefix on its output, and a progress bar for the download.
	if len(args) == 1 {
		name, ver, err := parseInstallArg(args[0])
		if err != nil {
			return err
		}
		commit, err := prepareInstallArg(cfg, name, ver, flagInstallDest)
		if err != nil {
			return err
		}
		hooks := appHooks(name, "")
		hooks.Force = flagInstallForce
		if ui.IsTTY() && !ui.Global.JSON {
			hooks.CosignMissing = cosignPrompt(cfg, name, os.Stdin)
		}
		res := ui.ActionResult{Name: name}
		hooks.OnResult = installResultHook(&res)
		progress := ui.NewProgressFn(name)
		err = install.Run(ctx, cfg, name, progress, hooks)
		if err == nil {
			err = commit(hooks.OnOK)
		}
		return finishSingle(&res, err)
	}

	if len(args) > 1 {
		// Validate every name before touching the manifest or installing
		// anything, so a typo in the last argument doesn't leave earlier
		// apps auto-imported into the manifest.
		names := make([]string, len(args))
		versions := make([]string, len(args))
		for i, arg := range args {
			name, ver, err := parseInstallArg(arg)
			if err != nil {
				return err
			}
			if err := validateAppName(cfg, name); err != nil {
				return err
			}
			names[i], versions[i] = name, ver
		}
		commits := make(map[string]func(func(string)) error, len(names))
		for i, name := range names {
			commit, err := prepareInstallArg(cfg, name, versions[i], "")
			if err != nil {
				return err
			}
			commits[name] = commit
		}
		return installParallel(ctx, cfg, names, commits)
	}

	// No args: install all apps from the manifest.
	if len(cfg.Apps) == 0 {
		if ui.Global.JSON {
			return printResults([]ui.ActionResult{})
		}
		fmt.Println("No apps configured in manifest (~/.config/paq/config.toml): nothing to install.")
		ui.Hint("install a tool with `paq install <name>` (find one with `paq search <name>`)")
		return nil
	}
	names := make([]string, 0, len(cfg.Apps))
	for name := range cfg.Apps {
		names = append(names, name)
	}
	return installParallel(ctx, cfg, names, nil)
}

// installParallel installs names concurrently (max maxParallel goroutines).
// commits (may be nil) holds, per app, the manifest write to make once its
// install succeeded; the writes are serialized, as they share one file.
func installParallel(ctx context.Context, cfg *config.Config, names []string, commits map[string]func(func(string)) error) error {
	var manifestMu sync.Mutex
	return runParallel(ctx, names, "installed", func(ctx context.Context, name string, hooks *install.Hooks, res *ui.ActionResult) error {
		hooks.Force = flagInstallForce
		hooks.OnResult = installResultHook(res)
		if err := install.Run(ctx, cfg, name, nil, hooks); err != nil {
			return err
		}
		commit := commits[name]
		if commit == nil {
			return nil
		}
		manifestMu.Lock()
		defer manifestMu.Unlock()
		return commit(hooks.OnOK)
	})
}

// installResultHook returns an OnResult hook that fills res from the
// pipeline's outcome.
func installResultHook(res *ui.ActionResult) func(install.Result) {
	return func(r install.Result) {
		res.Status, res.Version, res.Dest = "installed", r.Version, r.Dest
		if r.AlreadyInstalled {
			res.Status = "already_installed"
		}
	}
}

// finishSingle completes res with err (if any), prints it in --json mode and
// returns err.
func finishSingle(res *ui.ActionResult, err error) error {
	if err != nil {
		res.Status = "failed"
		res.Error, res.Hint = messageAndHint(err)
	}
	if perr := printResults([]ui.ActionResult{*res}); perr != nil && err == nil {
		return perr
	}
	return err
}

// printResults prints the per-app outcomes, sorted by name, in --json mode;
// human output already reported them line by line.
func printResults(results []ui.ActionResult) error {
	if !ui.Global.JSON {
		return nil
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return ui.PrintJSON(results)
}

// batchError summarizes the outcome of a parallel run. The per-app errors
// have already been shown individually, so the summary must not itself count
// as "already shown" (ErrAlreadyShown would then hide it) — which rules out
// exposing the underlying errors through Unwrap. A verification failure in any
// app must still drive the process exit code, so that one classification is
// propagated explicitly.
type batchError struct {
	msg string
	// failedVerification records that at least one app failed an integrity or
	// signature check, so the batch still exits with exitVerify.
	failedVerification bool
}

func (e batchError) Error() string { return e.msg }

func (e batchError) Is(target error) bool {
	return e.failedVerification && target == verify.ErrVerification
}

// runParallel runs action over names concurrently (max maxParallel
// goroutines), each with a [name]-prefixed, mutex-serialized set of hooks so
// output from different goroutines doesn't interleave.
//
// The apps are independent, so a failing one must not abort the others: every
// action runs to completion (only Ctrl-C, via ctx, cancels the batch) and the
// outcomes are collected into a final summary. verb names the successful
// outcome in that summary ("installed", "upgraded").
func runParallel(ctx context.Context, names []string, verb string, action func(context.Context, string, *install.Hooks, *ui.ActionResult) error) error {
	var (
		stdoutMu sync.Mutex
		done     int
		failed   []string
		results  []ui.ActionResult
		// Tracked here rather than re-derived from the summary text: the
		// per-app errors are not reachable from the returned batchError.
		failedVerification bool
	)

	var g errgroup.Group
	g.SetLimit(maxParallel)

	for _, name := range names {
		name := name // capture for the goroutine
		g.Go(func() error {
			prefix := fmt.Sprintf("[%-12s] ", name)
			hooks := lockedAppHooks(prefix, &stdoutMu)
			res := ui.ActionResult{Name: name}
			err := action(ctx, name, hooks, &res)

			stdoutMu.Lock()
			defer stdoutMu.Unlock()
			if err != nil {
				res.Status = "failed"
				res.Error, res.Hint = messageAndHint(err)
			}
			results = append(results, res)
			if err != nil {
				// The pipeline already shows the error via OnFail; we only
				// reprint it if for some reason it wasn't shown.
				if !install.ErrAlreadyShown(err) {
					ui.Fail("%s%v", prefix, err)
				}
				failed = append(failed, fmt.Sprintf("%s (%v)", name, err))
				if errors.Is(err, verify.ErrVerification) {
					failedVerification = true
				}
				return nil
			}
			done++
			return nil
		})
	}
	_ = g.Wait() // the goroutines never return an error: outcomes are collected above

	if err := printResults(results); err != nil {
		return err
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return batchError{
			msg:                fmt.Sprintf("%d %s, %d failed: %s", done, verb, len(failed), strings.Join(failed, "; ")),
			failedVerification: failedVerification,
		}
	}
	ui.OK("%d %s", done, verb)
	return nil
}

// parseInstallArg splits an install argument, "name" or "name@version".
func parseInstallArg(arg string) (name, version string, err error) {
	if strings.HasSuffix(arg, "@") {
		return "", "", fmt.Errorf("missing version after '@' in %q", arg)
	}
	name, version = parseAppRef(arg)
	return name, version, nil
}

// prepareInstallArg runs prepareManifestEntry for an install argument,
// honoring --no-save. The returned commit, to call once the install
// succeeded, writes the manifest and reports the write through ok.
func prepareInstallArg(cfg *config.Config, name, version, dest string) (commit func(ok func(string)) error, err error) {
	_, existed := cfg.Apps[name]
	persist, err := prepareManifestEntry(cfg, name, version, dest, !flagInstallNoSave)
	if err != nil {
		return nil, err
	}
	return func(ok func(string)) error {
		path, err := persist()
		if err != nil {
			return fmt.Errorf("%s is installed, but recording it in the manifest failed: %w", name, err)
		}
		switch {
		case path == "":
		case existed:
			ok(fmt.Sprintf("set %s version to %s in %s", name, version, path))
		default:
			ok(fmt.Sprintf("added %s to %s", name, path))
		}
		return nil
	}, nil
}

// ensureManifestEntry ensures that cfg.Apps[name] exists, so that install
// can proceed. If name is missing from the manifest but matches a registry
// spec, it synthesizes a default entry (auto-import): injects it into
// cfg.Apps in memory and, if save is true, persists it to the user manifest.
// A non-empty version (from "name@version") overrides the entry's version:
// the default one of an auto-imported entry, or the manifest's one of an
// existing entry, whose version line is then rewritten if save is true.
// A non-empty dest (from --dest) overrides the default destination of an
// auto-imported entry; for an existing entry it only applies to an unsaved
// run, since the manifest's own dest is not rewritten.
// Returns the written path ("" if nothing was persisted) or a hintError if
// name is neither a known app nor a known spec.
func ensureManifestEntry(cfg *config.Config, name, version, dest string, save bool) (string, error) {
	persist, err := prepareManifestEntry(cfg, name, version, dest, save)
	if err != nil {
		return "", err
	}
	return persist()
}

// prepareManifestEntry is ensureManifestEntry without the write: it validates
// and updates cfg.Apps in memory, and returns the function that persists the
// change (a no-op returning "" when there is nothing to save). Install calls
// it only once the tool is on disk, so a failed install leaves the manifest
// untouched.
func prepareManifestEntry(cfg *config.Config, name, version, dest string, save bool) (persist func() (string, error), err error) {
	nothing := func() (string, error) { return "", nil }
	if entry, exists := cfg.Apps[name]; exists {
		if dest != "" && dest != entry.Dest {
			if save {
				return nil, fmt.Errorf("%q is already in the manifest with dest %q: use --no-save to install it elsewhere for this run, or change its dest in the manifest", name, entry.Dest)
			}
			entry.Dest = dest
			cfg.Apps[name] = entry
		}
		if version == "" || version == entry.Version {
			return nothing, nil
		}
		entry.Version = version
		cfg.Apps[name] = entry
		if !save {
			return nothing, nil
		}
		return func() (string, error) {
			path, err := config.SetManifestAppVersion(name, version)
			if err != nil {
				return "", fmt.Errorf("update manifest entry: %w", err)
			}
			return path, nil
		}, nil
	}

	if err := validateAppName(cfg, name); err != nil {
		return nil, err
	}

	spec := cfg.Specs[name]
	entry := config.AppEntry{
		Use:     name,
		Version: defaultImportVersion(spec),
		Dest:    config.DefaultDest(spec, name, cfg.Defaults),
	}
	if version != "" {
		entry.Version = version
	}
	if dest != "" {
		entry.Dest = dest
	}
	// Makes the app installable in memory, regardless of persistence.
	cfg.Apps[name] = entry

	if !save {
		return nothing, nil
	}

	return func() (string, error) {
		path, err := config.WriteManifestEntry(name, renderAppEntryTOML(name, entry), false)
		if err != nil {
			return "", fmt.Errorf("write manifest entry: %w", err)
		}
		return path, nil
	}, nil
}

// validateAppName reports whether name can be installed: either it's already
// in the manifest, or it matches a known registry spec with a valid app key.
// Used to fail fast on a typo before installing or auto-importing anything.
func validateAppName(cfg *config.Config, name string) error {
	if _, exists := cfg.Apps[name]; exists {
		return nil
	}
	if _, ok := cfg.Specs[name]; !ok {
		return hintError{
			msg:          fmt.Sprintf("%q is not in your manifest and not a known registry spec", name),
			hint:         specNotFoundHint(cfg.Specs, name),
			suggestLocal: true,
		}
	}
	if !validAppKey(name) {
		return fmt.Errorf("invalid app name %q: use only letters, digits, '-' or '_'", name)
	}
	return nil
}

// appHooks builds the Hooks for a single app (used for single-app install).
func appHooks(name, prefix string) *install.Hooks {
	return &install.Hooks{
		OnStep:        func(msg string) { ui.Step("%s%s", prefix, msg) },
		OnOK:          func(msg string) { ui.OK("%s%s", prefix, msg) },
		OnFail:        func(err error) { ui.Fail("%s%v", prefix, err) },
		OnInfo:        func(msg string) { ui.Info("%s%s", prefix, msg) },
		OnWarn:        func(msg string) { ui.Warn("%s%s", prefix, msg) },
		OnDebug:       func(msg string) { ui.Debug("%s%s", prefix, msg) },
		RequireVerify: requireVerify(),
	}
}

// requireVerify reports whether strict verification is on: --require-verify,
// or PAQ_REQUIRE_VERIFY set to any value but "", "0" or "false" (a typo must
// not silently disable a security setting).
func requireVerify() bool {
	if flagInstallRequireVerify {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PAQ_REQUIRE_VERIFY"))) {
	case "", "0", "false":
		return false
	}
	return true
}

// cosignPrompt returns the Hooks.CosignMissing callback for an interactive
// session: when a keyless signature needs cosign and none is available, it
// asks whether to install cosign as a regular tool (recorded in the manifest
// and state, like "paq install cosign"), to let paq use its private copy (the
// default), or to abort.
func cosignPrompt(cfg *config.Config, app string, r io.Reader) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		fmt.Printf("cosign is required to verify %s. Install it as [u]ser tool, [p]rivate copy for paq, or [a]bort? [P/u/a] ", app)
		line, _ := bufio.NewReader(r).ReadString('\n')
		switch strings.TrimSpace(strings.ToLower(line)) {
		case "u", "user":
			commit, err := prepareInstallArg(cfg, "cosign", "", "")
			if err != nil {
				return false, err
			}
			hooks := appHooks("cosign", "")
			if err := install.Run(ctx, cfg, "cosign", ui.NewProgressFn("cosign"), hooks); err != nil {
				return false, err
			}
			if err := commit(hooks.OnOK); err != nil {
				return false, err
			}
			return true, nil
		case "a", "abort":
			return false, install.ErrCosignDeclined
		}
		return false, nil
	}
}

// lockedAppHooks builds Hooks serialized on a shared mutex, to avoid
// interleaved output during parallel operations (install/upgrade of multiple apps).
func lockedAppHooks(prefix string, mu *sync.Mutex) *install.Hooks {
	return &install.Hooks{
		OnStep:        func(msg string) { mu.Lock(); ui.Step("%s%s", prefix, msg); mu.Unlock() },
		OnOK:          func(msg string) { mu.Lock(); ui.OK("%s%s", prefix, msg); mu.Unlock() },
		OnFail:        func(err error) { mu.Lock(); ui.Fail("%s%v", prefix, err); mu.Unlock() },
		OnInfo:        func(msg string) { mu.Lock(); ui.Info("%s%s", prefix, msg); mu.Unlock() },
		OnWarn:        func(msg string) { mu.Lock(); ui.Warn("%s%s", prefix, msg); mu.Unlock() },
		OnDebug:       func(msg string) { mu.Lock(); ui.Debug("%s%s", prefix, msg); mu.Unlock() },
		RequireVerify: requireVerify(),
	}
}

// loadConfig loads the registry + global templates + user manifest and merges them.
func loadConfig() (*config.Config, error) {
	cfg, _, err := loadConfigWithMeta()
	return cfg, err
}

// loadConfigWithMeta is loadConfig plus the metadata of the external registry
// snapshot in use (nil when running on the embedded registry only).
// A missing or broken snapshot never fails the load: it degrades to the
// embedded registry with a warning on stderr. No network is ever involved.
func loadConfigWithMeta() (*config.Config, *registry.Meta, error) {
	specs, err := config.LoadEmbeddedRegistry(embedded.RegistryFS)
	if err != nil {
		return nil, nil, fmt.Errorf("load registry: %w", err)
	}

	globalTmpl, globalTmplOS, err := config.LoadGlobalTemplates(embedded.RegistryFS)
	if err != nil {
		return nil, nil, fmt.Errorf("load global templates: %w", err)
	}

	var regMeta *registry.Meta
	snapFS, meta, err := registry.Open()
	if err != nil {
		ui.Warn("external registry cache is unusable (%v): using the embedded registry, run `paq registry update` to refresh it", err)
	} else if snapFS != nil {
		if err := config.OverlayRegistry(specs, globalTmpl, globalTmplOS, snapFS); err != nil {
			ui.Warn("external registry cache is unusable (%v): using the embedded registry, run `paq registry update` to refresh it", err)
		} else {
			regMeta = meta
		}
	}

	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("load user config: %w", err)
	}
	userCfg.GlobalTemplates = globalTmpl
	userCfg.GlobalTemplatesOS = globalTmplOS

	cfg, err := config.Merge(specs, userCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("merge config: %w", err)
	}

	lock, err := config.LoadLock()
	if err != nil {
		return nil, nil, fmt.Errorf("load lockfile: %w", err)
	}
	cfg.Lock = lock

	return cfg, regMeta, nil
}
