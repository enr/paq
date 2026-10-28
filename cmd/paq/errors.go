package main

import (
	"errors"
	"strings"

	"github.com/enr/paq/internal/ghtoken"
	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/ui"
)

// hintError carries an error message together with an actionable hint.
// Commands use it instead of ui.Fail+ui.Hint+os.Exit so that errors propagate
// normally and reportError can print the hint through the central path.
type hintError struct {
	msg  string
	hint string
}

func (e hintError) Error() string { return e.msg }

// reportError is the single point of presentation for CLI errors.
// Prints the error in red (if not already shown by the pipeline) and, when
// possible, a concrete suggestion on how to resolve it.
func reportError(err error) {
	if err == nil {
		return
	}
	var he hintError
	if errors.As(err, &he) {
		ui.Fail("%s", he.msg)
		if he.hint != "" {
			ui.Hint("%s", he.hint)
		}
		return
	}
	if !install.ErrAlreadyShown(err) {
		ui.Fail("%v", err)
	}
	if hint := hintFor(err); hint != "" {
		ui.Hint("%s", hint)
	}
}

// hintFor returns an actionable suggestion based on the error's content.
// The heuristics rely on the messages produced by the various layers
// (pipeline, download, version provider, ...).
func hintFor(err error) string {
	msg := strings.ToLower(err.Error())

	switch {
	// Security: invalid integrity or signature. The most important case.
	case strings.Contains(msg, "sha256 mismatch"),
		strings.Contains(msg, "sha512 mismatch"),
		strings.Contains(msg, "integrity check"):
		return "the downloaded file does not match its expected checksum — it may be corrupted or tampered with. " +
			"Retry the install; if it keeps failing, do NOT use the binary and report it upstream."

	case strings.Contains(msg, "signature"):
		return "the cryptographic signature could not be verified — the file or the configured public key may be wrong. " +
			"Do NOT trust the binary until this passes."

	case strings.Contains(msg, "known vulnerabilities found"):
		return "upgrade the affected tools (`paq upgrade`) or pin a fixed version; " +
			"look up an ID at https://osv.dev/vulnerability/<id>, or list them all with --json."

	// Missing configuration.
	case strings.Contains(msg, "not found in manifest"):
		return "add the app to your manifest (~/.config/paq/config.toml), e.g.\n" +
			"    [apps.<name>]\n    use = \"<spec>\"\n    version = \"latest\""

	case strings.Contains(msg, "not found in registry"):
		return "list available specs with `paq registry`, and check the `use` field of the app in your manifest."

	case strings.Contains(msg, "unknown backend"):
		return "check the spec's `backend` field — supported values are \"github\" and \"url\"."

	// Version resolution / GitHub API.
	// With a token, GitHub answers 404 (not 403) for a repo the token cannot
	// read, so the token is as likely a culprit as the repo name.
	case strings.Contains(msg, "github api returned 404") && ghtoken.For(ghtoken.RepoFrom(msg)) != "":
		return "the repository or release was not found with the GitHub token in use — " +
			"check the spec `repo` and that the release exists, then that the token can read the repository " +
			"(a fine-grained token only covers the repos of its resource owner). " +
			"A spec can name its own token variable with `github_token_env`."

	case strings.Contains(msg, "github api returned 404"),
		strings.Contains(msg, "github api returned"):
		return "check the repository name in the spec; if you are rate-limited, set the GITHUB_TOKEN environment variable."

	case strings.Contains(msg, "resolve version"),
		strings.Contains(msg, "resolve latest version"):
		return "verify the spec `repo` and your network connection; `latest` requires a GitHub-backed spec."

	// Platform not supported by the recipe.
	case strings.Contains(msg, "is not available for"):
		return "this tool has no build for your OS/architecture; " +
			"see supported platforms with `paq registry show <name>`."

	// Failed download (artifact or auxiliary assets).
	case strings.Contains(msg, "asset") && strings.Contains(msg, "not found"):
		return "the release exists but has no matching asset — check the `asset` template, the version, and your OS/arch."

	// Without a token, a private repo's release download 404s just like a
	// wrong repo or version: GitHub does not reveal which one it is.
	case strings.Contains(msg, "http 404") && strings.Contains(msg, "github.com/") &&
		strings.Contains(msg, "/releases/download/") && ghtoken.For(ghtoken.RepoFrom(msg)) == "":
		return "the release file was not found — check the spec `repo` and the version; " +
			"if the repository is private, set the GITHUB_TOKEN environment variable."

	case strings.Contains(msg, "http 404"),
		strings.Contains(msg, "download") && strings.Contains(msg, "http"):
		return "the file was not found at the resolved URL — verify the version and that a build exists for your platform. " +
			"Re-run with --debug to see the exact URLs."

	// Destination permissions.
	case strings.Contains(msg, "permission denied"):
		return "paq cannot write to the destination — choose a writable `dest` or adjust permissions."
	}

	// Generic suggestion: point to --debug for diagnosis.
	if !ui.Global.Debug {
		return "re-run with --debug for a detailed trace of what paq attempted."
	}
	return ""
}
