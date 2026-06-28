package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/state"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	nameStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	labelStyle  = lipgloss.NewStyle().Bold(true).Width(16)
)

// PrintLsTable stampa la tabella dei pacchetti installati.
func PrintLsTable(packages []state.InstalledApp) {
	if Global.JSON {
		data, _ := json.MarshalIndent(packages, "", "  ")
		fmt.Println(string(data))
		return
	}

	pkgs := sortedPackages(packages)

	if !IsColorEnabled() {
		// Output plain text
		fmt.Printf("%-20s %-12s %-8s %s\n", "NAME", "VERSION", "KIND", "DEST")
		fmt.Printf("%-20s %-12s %-8s %s\n", "----", "-------", "----", "----")
		for _, rec := range pkgs {
			fmt.Printf("%-20s %-12s %-8s %s\n", rec.Name, rec.Version, rec.Kind, rec.Dest)
		}
		return
	}

	header := fmt.Sprintf("%s  %s  %s  %s",
		headerStyle.Width(20).Render("NAME"),
		headerStyle.Width(12).Render("VERSION"),
		headerStyle.Width(8).Render("KIND"),
		headerStyle.Render("DEST"),
	)
	fmt.Println(header)

	for _, rec := range pkgs {
		row := fmt.Sprintf("%s  %s  %s  %s",
			nameStyle.Width(20).Render(rec.Name),
			lipgloss.NewStyle().Width(12).Render(rec.Version),
			dimStyle.Width(8).Render(rec.Kind),
			dimStyle.Render(rec.Dest),
		)
		fmt.Println(row)
	}
}

// RegistryEntry è una riga della tabella delle spec disponibili nella registry.
type RegistryEntry struct {
	Name    string `json:"name"`
	Backend string `json:"backend"`
	Repo    string `json:"repo"`
}

// PrintAvailableTable stampa la tabella delle spec disponibili nella registry embedded.
func PrintAvailableTable(entries []RegistryEntry) {
	if Global.JSON {
		data, _ := json.MarshalIndent(entries, "", "  ")
		fmt.Println(string(data))
		return
	}

	cell := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}

	if !IsColorEnabled() {
		fmt.Printf("%-20s %-8s %s\n", "NAME", "BACKEND", "REPO")
		fmt.Printf("%-20s %-8s %s\n", "----", "-------", "----")
		for _, r := range entries {
			fmt.Printf("%-20s %-8s %s\n", r.Name, cell(r.Backend), cell(r.Repo))
		}
		return
	}

	header := fmt.Sprintf("%s  %s  %s",
		headerStyle.Width(20).Render("NAME"),
		headerStyle.Width(8).Render("BACKEND"),
		headerStyle.Render("REPO"),
	)
	fmt.Println(header)

	for _, r := range entries {
		row := fmt.Sprintf("%s  %s  %s",
			nameStyle.Width(20).Render(r.Name),
			dimStyle.Width(8).Render(cell(r.Backend)),
			dimStyle.Render(cell(r.Repo)),
		)
		fmt.Println(row)
	}
}

// PrintConfigShow stampa il path della configurazione utente valutata e i suoi
// dati: i default effettivi (configurati o built-in) e le app dichiarate.
func PrintConfigShow(path string, exists bool, defaults config.Defaults, effBin, effOpt string, apps map[string]config.AppEntry) {
	if Global.JSON {
		out := map[string]any{
			"path":               path,
			"exists":             exists,
			"defaults":           map[string]string{"bin": defaults.Bin, "opt": defaults.Opt},
			"effective_defaults": map[string]string{"bin": effBin, "opt": effOpt},
			"apps":               apps,
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		return
	}

	// Chiave in blu grassetto, valore in verde, annotazioni in grigio, così da
	// distinguere a colpo d'occhio etichette e valori.
	keyStyle := labelStyle.Foreground(lipgloss.Color("12"))
	val := func(s string) string {
		if IsColorEnabled() {
			return nameStyle.Render(s)
		}
		return s
	}
	dim := func(s string) string {
		if IsColorEnabled() {
			return dimStyle.Render(s)
		}
		return s
	}
	render := func(label, value string) {
		if IsColorEnabled() {
			fmt.Fprintf(os.Stdout, "%s %s\n", keyStyle.Render(label+":"), value)
		} else {
			fmt.Printf("%-16s %s\n", label+":", value)
		}
	}
	section := func(title string) {
		if IsColorEnabled() {
			fmt.Println(headerStyle.Render(title))
		} else {
			fmt.Println(title)
		}
	}
	source := func(configured string) string {
		if configured != "" {
			return "(from [defaults])"
		}
		return "(built-in)"
	}

	fmt.Println()
	if exists {
		render("Config", val(path))
	} else {
		render("Config", val(path)+" "+dim("(not found — using built-in defaults)"))
	}

	fmt.Println()
	section("Defaults")
	render("bin", fmt.Sprintf("%s  %s", val(effBin), dim(source(defaults.Bin))))
	render("opt", fmt.Sprintf("%s  %s", val(effOpt), dim(source(defaults.Opt))))

	fmt.Println()
	if len(apps) == 0 {
		section("Apps")
		fmt.Println(dim("(none configured)"))
		return
	}
	section(fmt.Sprintf("Apps (%d)", len(apps)))

	keys := make([]string, 0, len(apps))
	for k := range apps {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	header := fmt.Sprintf("%s  %s  %s  %s",
		headerStyle.Width(16).Render("APP"),
		headerStyle.Width(16).Render("USE"),
		headerStyle.Width(10).Render("VERSION"),
		headerStyle.Render("DEST"),
	)
	if IsColorEnabled() {
		fmt.Println(header)
	} else {
		fmt.Printf("%-16s %-16s %-10s %s\n", "APP", "USE", "VERSION", "DEST")
	}
	for _, k := range keys {
		a := apps[k]
		use := a.Use
		if use == "" {
			use = k
		}
		ver := a.Version
		if ver == "" {
			ver = "(default)"
		}
		dest := a.Dest
		if dest == "" {
			dest = "(default)"
		}
		if IsColorEnabled() {
			fmt.Printf("%s  %s  %s  %s\n",
				nameStyle.Width(16).Render(k),
				dimStyle.Width(16).Render(use),
				lipgloss.NewStyle().Width(10).Render(ver),
				dimStyle.Render(dest),
			)
		} else {
			fmt.Printf("%-16s %-16s %-10s %s\n", k, use, ver, dest)
		}
	}
}

// PrintInfoDetail stampa i dettagli di un'app (ricetta + versioni installate).
// installed contiene tutte le versioni dell'app presenti nello state (può essere vuoto).
func PrintInfoDetail(name string, spec config.Spec, app config.AppEntry, installed []state.InstalledApp) {
	if Global.JSON {
		out := map[string]any{
			"name":       name,
			"spec": spec,
			"app":        app,
			"installed":  installed,
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		return
	}

	render := func(label, value string) {
		if value == "" {
			return
		}
		if IsColorEnabled() {
			fmt.Fprintf(os.Stdout, "%s %s\n", labelStyle.Render(label+":"), value)
		} else {
			fmt.Printf("%-16s %s\n", label+":", value)
		}
	}

	fmt.Println()
	render("App", name)
	render("Version", app.Version)
	render("Dest", app.Dest)
	fmt.Println()
	render("Spec", app.Use)
	render("Backend", spec.Backend)
	render("Repo", spec.Repo)
	render("Source", spec.Source)
	render("Asset", spec.Asset)
	render("Archive", spec.Archive)
	render("Extract", spec.Extract)
	render("Binaries", strings.Join(formatBinaries(spec.Binaries), ", "))
	if spec.StripComponents > 0 {
		render("Strip", fmt.Sprintf("%d", spec.StripComponents))
	}
	render("Subdir", spec.Subdir)
	render("SHA256", spec.Verify.SHA256)
	render("SHA256Asset", spec.Verify.SHA256Asset)

	if len(installed) == 0 {
		fmt.Println()
		fmt.Println("(not installed)")
		return
	}

	for _, rec := range sortedPackages(installed) {
		fmt.Println()
		render("Installed ver", rec.Version)
		render("Installed at", rec.InstalledAt.Format("2006-01-02 15:04:05"))
		render("Kind", rec.Kind)
		render("Dest", rec.Dest)
		render("Source URL", rec.Source)
		render("SHA256", rec.SHA256)
	}
}

// PrintSpecDetail stampa i dettagli di una singola spec della registry.
func PrintSpecDetail(name string, spec config.Spec) {
	if Global.JSON {
		out := map[string]any{
			"name":       name,
			"spec": spec,
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		return
	}

	render := func(label, value string) {
		if value == "" {
			return
		}
		// Un'etichetta vuota (riga di continuazione) viene allineata senza i due punti.
		text := label
		if label != "" {
			text = label + ":"
		}
		if IsColorEnabled() {
			fmt.Fprintf(os.Stdout, "%s %s\n", labelStyle.Render(text), value)
		} else {
			fmt.Printf("%-16s %s\n", text, value)
		}
	}

	renderMap := func(label string, m map[string]string) {
		if len(m) == 0 {
			return
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		first := true
		for _, k := range keys {
			if first {
				render(label, fmt.Sprintf("%s = %s", k, m[k]))
				first = false
			} else {
				render("", fmt.Sprintf("%s = %s", k, m[k]))
			}
		}
	}

	fmt.Println()
	render("Spec", name)
	render("Backend", spec.Backend)
	render("Repo", spec.Repo)
	render("Default version", spec.DefaultVersion)
	render("Source", spec.Source)
	render("Asset", spec.Asset)
	render("Archive", spec.Archive)
	render("Extract", spec.Extract)
	for i, b := range formatBinaries(spec.Binaries) {
		if i == 0 {
			render("Binaries", b)
		} else {
			render("", b)
		}
	}
	if spec.StripComponents > 0 {
		render("Strip", fmt.Sprintf("%d", spec.StripComponents))
	}
	render("Subdir", spec.Subdir)
	render("Chmod", spec.Chmod)
	renderMap("OS", spec.OS)
	renderMap("Arch", spec.Arch)
	renderMap("Env", spec.Env)
	if len(spec.Platforms) > 0 {
		render("Platforms", strings.Join(spec.Platforms, ", "))
	}
	render("SHA256", spec.Verify.SHA256)
	render("SHA256Asset", spec.Verify.SHA256Asset)
	render("Minisign key", spec.Verify.Minisign.PublicKey)
	render("Minisign sig", spec.Verify.Minisign.SignedAsset)
}

// formatBinaries rende ogni Binary in "from → to" (o solo "from"/"to" quando
// uno dei due manca, es. download nudo). Ritorna nil se la lista è vuota.
func formatBinaries(bins []config.Binary) []string {
	if len(bins) == 0 {
		return nil
	}
	out := make([]string, 0, len(bins))
	for _, b := range bins {
		switch {
		case b.From != "" && b.To != "":
			out = append(out, b.From+" → "+b.To)
		case b.From != "":
			out = append(out, b.From)
		default:
			out = append(out, b.To)
		}
	}
	return out
}

// sortedPackages ritorna una copia ordinata per nome poi versione.
func sortedPackages(in []state.InstalledApp) []state.InstalledApp {
	out := make([]state.InstalledApp, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}
