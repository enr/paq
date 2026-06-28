package main

import (
	"strings"

	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/ui"
)

// reportError è il punto unico di presentazione degli errori del CLI.
// Stampa l'errore in rosso (se non già mostrato dalla pipeline) e, quando
// possibile, un suggerimento concreto su come risolverlo.
func reportError(err error) {
	if err == nil {
		return
	}
	if !install.ErrAlreadyShown(err) {
		ui.Fail("%v", err)
	}
	if hint := hintFor(err); hint != "" {
		ui.Hint("%s", hint)
	}
}

// hintFor restituisce un suggerimento azionabile in base al contenuto
// dell'errore. Le euristiche si basano sui messaggi prodotti dai vari layer
// (pipeline, download, version provider, ...).
func hintFor(err error) string {
	msg := strings.ToLower(err.Error())

	switch {
	// Sicurezza: integrità o firma non valide. È il caso più importante.
	case strings.Contains(msg, "sha256 mismatch"),
		strings.Contains(msg, "sha512 mismatch"),
		strings.Contains(msg, "integrity check"):
		return "the downloaded file does not match its expected checksum — it may be corrupted or tampered with. " +
			"Retry the install; if it keeps failing, do NOT use the binary and report it upstream."

	case strings.Contains(msg, "signature"):
		return "the cryptographic signature could not be verified — the file or the configured public key may be wrong. " +
			"Do NOT trust the binary until this passes."

	// Configurazione mancante.
	case strings.Contains(msg, "not found in manifest"):
		return "add the app to your manifest (~/.config/paq/config.toml), e.g.\n" +
			"    [apps.<name>]\n    use = \"<spec>\"\n    version = \"latest\""

	case strings.Contains(msg, "not found in registry"):
		return "list available specs with `paq registry`, and check the `use` field of the app in your manifest."

	// Risoluzione versione / API GitHub.
	case strings.Contains(msg, "github api returned 404"),
		strings.Contains(msg, "github api returned"):
		return "check the repository name in the spec; if you are rate-limited, set the GITHUB_TOKEN environment variable."

	case strings.Contains(msg, "resolve version"),
		strings.Contains(msg, "resolve latest version"):
		return "verify the spec `repo` and your network connection; `latest` requires a GitHub-backed spec."

	// Piattaforma non supportata dalla ricetta.
	case strings.Contains(msg, "is not available for"):
		return "this tool has no build for your OS/architecture; " +
			"see supported platforms with `paq registry show <name>`."

	// Download fallito (artefatto o asset ausiliari).
	case strings.Contains(msg, "asset") && strings.Contains(msg, "not found"):
		return "the release exists but has no matching asset — check the `asset` template, the version, and your OS/arch."

	case strings.Contains(msg, "http 404"),
		strings.Contains(msg, "download") && strings.Contains(msg, "http"):
		return "the file was not found at the resolved URL — verify the version and that a build exists for your platform. " +
			"Re-run with --debug to see the exact URLs."

	// Permessi sulla destinazione.
	case strings.Contains(msg, "permission denied"):
		return "paq cannot write to the destination — choose a writable `dest` or adjust permissions."
	}

	// Suggerimento generico: indirizza verso --debug per la diagnosi.
	if !ui.Global.Debug {
		return "re-run with --debug for a detailed trace of what paq attempted."
	}
	return ""
}
