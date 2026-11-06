package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/enr/paq/internal/ghtoken"
	"github.com/enr/paq/internal/httperr"
	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/ui"
)

// hintError carries an error message together with an actionable hint.
// Commands use it instead of ui.Fail+ui.Hint+os.Exit so that errors propagate
// normally and reportError can print the hint through the central path.
type hintError struct {
	msg  string
	hint string
	// suggestLocal marks a "not found" error that a forgotten --local can
	// explain: next to a project manifest, the hint also suggests it.
	suggestLocal bool
}

func (e hintError) Error() string { return e.msg }

// reportError is the single point of presentation for CLI errors.
// Prints the error in red (if not already shown by the pipeline) and, when
// possible, a concrete suggestion on how to resolve it. With --json it also
// prints the error as a JSON document on stdout, unless the command already
// printed its own result there.
func reportError(err error) {
	if err == nil {
		return
	}
	// The command run by `paq exec` reported its own failure.
	if errors.As(err, new(childExitError)) {
		return
	}
	msg, hint := messageAndHint(err)
	if jsonRequested() && !ui.JSONWritten {
		code := exitCodeFor(err)
		_ = ui.PrintJSON(jsonErrorDoc{Error: jsonErrorBody{
			Code: exitCodeName(code), ExitCode: code, Message: msg, Hint: hint,
		}})
	}
	if errors.As(err, new(hintError)) || !install.ErrAlreadyShown(err) {
		ui.Fail("%s", msg)
	}
	if hint != "" {
		ui.Hint("%s", hint)
	}
}

// messageAndHint returns the message to show for err and a suggestion on how
// to fix it: a hintError's own, or one derived from the error.
func messageAndHint(err error) (msg, hint string) {
	var he hintError
	if errors.As(err, &he) {
		hint = he.hint
		if lh := localModeHintIf(he.suggestLocal); lh != "" {
			hint = joinHints(hint, lh)
		}
		return he.msg, hint
	}
	return err.Error(), hintFor(err)
}

// localModeHintIf returns localModeHint() when want is true.
func localModeHintIf(want bool) string {
	if !want {
		return ""
	}
	return localModeHint()
}

// joinHints combines two suggestions into one hint line.
func joinHints(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; or " + b
}

// jsonErrorDoc is the document --json prints on stdout when a command fails
// before producing its own output.
type jsonErrorDoc struct {
	Error jsonErrorBody `json:"error"`
}

type jsonErrorBody struct {
	// Code names the failure class; it maps one-to-one to ExitCode.
	Code     string `json:"code"`
	ExitCode int    `json:"exit_code"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

// jsonRequested reports whether the user asked for --json. It also scans the
// raw arguments, because an unknown command or flag fails before cobra has
// parsed --json.
func jsonRequested() bool {
	if ui.Global.JSON || flagJSON {
		return true
	}
	for _, a := range os.Args[1:] {
		if a == "--" {
			break
		}
		if a == "--json" || a == "-j" || a == "--json=true" {
			return true
		}
	}
	return false
}

// hintFor returns an actionable suggestion based on the error's content.
// The heuristics rely on the messages produced by the various layers
// (pipeline, download, version provider, ...).
func hintFor(err error) string {
	msg := strings.ToLower(err.Error())
	var statusErr *httperr.StatusError

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
		return "list available specs with `paq search`, and check the `use` field of the app in your manifest."

	case strings.Contains(msg, "unknown backend"):
		return "check the spec's `backend` field — supported values are \"github\", \"url\" and \"npm\"."

	// A server answered with an unexpected HTTP status (GitHub API, release
	// or archive download).
	case errors.As(err, &statusErr):
		return statusHint(statusErr)

	case strings.Contains(msg, "resolve version"),
		strings.Contains(msg, "resolve latest version"):
		return "verify the spec `repo` (or npm `package`) and your network connection."

	// Platform not supported by the recipe.
	case strings.Contains(msg, "is not available for"):
		return "this tool has no build for your OS/architecture; " +
			"see supported platforms with `paq registry show <name>`."

	// Failed download (artifact or auxiliary assets).
	case strings.Contains(msg, "asset") && strings.Contains(msg, "not found"):
		return "the release exists but has no matching asset — check the `asset` template, the version, and your OS/arch."

	// Destination permissions.
	case strings.Contains(msg, "permission denied"):
		return "paq cannot write to the destination — choose a writable `dest` or adjust permissions."

	// Wrong CLI usage: the command's help is what fixes it, not a trace.
	case isUsageError(err):
		return fmt.Sprintf("see `%s --help` for usage.", usageCommandPath())
	}

	// Generic suggestion: point to --debug for diagnosis.
	if !ui.Global.Debug {
		return "re-run with --debug for a detailed trace of what paq attempted."
	}
	return ""
}

// statusHint suggests a fix for an unexpected HTTP status. Whether a GitHub
// token is in use decides between "set a token" and "check the token": the
// same 403 or 404 means different things with and without one.
func statusHint(e *httperr.StatusError) string {
	repo := ghtoken.RepoFrom(e.URL)
	onGitHub := repo != ""
	withToken := onGitHub && ghtoken.For(repo) != ""

	switch {
	case onGitHub && e.Code == http.StatusUnauthorized:
		return "GitHub rejected the token in use — it may be expired or revoked. " +
			"Check GITHUB_TOKEN (or the variable named by the spec's `github_token_env`)."

	case onGitHub && (e.Code == http.StatusForbidden || e.Code == http.StatusTooManyRequests) && withToken:
		return "GitHub refused the request even with the token in use — the token may have hit its own rate limit, " +
			"lack access to the repository, or be blocked by a proxy. Re-run with --debug to see the requests."

	case onGitHub && (e.Code == http.StatusForbidden || e.Code == http.StatusTooManyRequests):
		return "GitHub is rate-limiting unauthenticated requests — set the GITHUB_TOKEN environment variable to raise the limit."

	// With a token, GitHub answers 404 (not 403) for a repo the token cannot
	// read, so the token is as likely a culprit as the repo name.
	case e.Code == http.StatusNotFound && withToken:
		return "the repository or release was not found with the GitHub token in use — " +
			"check the spec `repo` and that the release exists, then that the token can read the repository " +
			"(a fine-grained token only covers the repos of its resource owner). " +
			"A spec can name its own token variable with `github_token_env`."

	// Without a token, a private repo 404s just like a wrong repo or
	// version: GitHub does not reveal which one it is.
	case e.Code == http.StatusNotFound && onGitHub:
		return "the repository, release or file was not found — check the spec `repo` and the version; " +
			"if the repository is private, set the GITHUB_TOKEN environment variable."

	case e.Code == http.StatusNotFound:
		return "the file was not found at the resolved URL — verify the version and that a build exists for your platform. " +
			"Re-run with --debug to see the exact URLs."

	case e.Code >= 500:
		return "the server had an internal error — it is likely temporary; retry in a few minutes."
	}
	return "the server refused the request — re-run with --debug to see the exact URL and response."
}

// usageCommandPath returns the path of the command the user was invoking
// (e.g. "paq install"), falling back to "paq" when it cannot be resolved.
func usageCommandPath() string {
	if cmd, _, err := rootCmd.Find(os.Args[1:]); err == nil {
		return cmd.CommandPath()
	}
	return rootCmd.CommandPath()
}
