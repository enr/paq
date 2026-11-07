package ui

import (
	"encoding/json"
	"fmt"
	"os"
)

// JSONWritten records that a JSON document was printed to stdout, so the
// error path does not append a second one: with --json, stdout holds exactly
// one document.
var JSONWritten bool

// PrintJSON prints v to stdout as indented JSON. HTML escaping is off, so a
// URL keeps its "&" instead of turning into "\u0026".
func PrintJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	JSONWritten = true
	return nil
}

// ActionResult is the per-app outcome of install, upgrade and uninstall in
// --json mode.
type ActionResult struct {
	Name string `json:"name"`
	// Status is one of: installed, already_installed, upgraded, up_to_date,
	// skipped, uninstalled, would_uninstall, failed.
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Dest    string `json:"dest,omitempty"`
	// Reason explains a skipped app.
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
	Hint   string `json:"hint,omitempty"`
}
