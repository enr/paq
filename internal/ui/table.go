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

// PrintLsTable prints the table of installed packages.
func PrintLsTable(packages []state.InstalledApp) {
	if Global.JSON {
		data, _ := json.MarshalIndent(packages, "", "  ")
		fmt.Println(string(data))
		return
	}

	pkgs := sortedPackages(packages)

	headers := []string{"NAME", "VERSION", "KIND"}
	rows := make([][]string, len(pkgs))
	for i, rec := range pkgs {
		rows[i] = []string{rec.Name, rec.Version, rec.Kind}
	}
	w := colWidths(headers, rows)

	if !IsColorEnabled() {
		// Output plain text
		fmtStr := fmt.Sprintf("%%-%ds %%-%ds %%-%ds %%s\n", w[0], w[1], w[2])
		fmt.Printf(fmtStr, headers[0], headers[1], headers[2], "DEST")
		fmt.Printf(fmtStr, strings.Repeat("-", w[0]), strings.Repeat("-", w[1]), strings.Repeat("-", w[2]), "----")
		for _, rec := range pkgs {
			fmt.Printf(fmtStr, rec.Name, rec.Version, rec.Kind, rec.Dest)
		}
		return
	}

	header := fmt.Sprintf("%s  %s  %s  %s",
		headerStyle.Width(w[0]).Render(headers[0]),
		headerStyle.Width(w[1]).Render(headers[1]),
		headerStyle.Width(w[2]).Render(headers[2]),
		headerStyle.Render("DEST"),
	)
	fmt.Println(header)

	for _, rec := range pkgs {
		row := fmt.Sprintf("%s  %s  %s  %s",
			nameStyle.Width(w[0]).Render(rec.Name),
			lipgloss.NewStyle().Width(w[1]).Render(rec.Version),
			dimStyle.Width(w[2]).Render(rec.Kind),
			dimStyle.Render(rec.Dest),
		)
		fmt.Println(row)
	}
}

// RegistryEntry is a row of the table of specs available in the registry.
type RegistryEntry struct {
	Name    string `json:"name"`
	Backend string `json:"backend"`
	Repo    string `json:"repo"`
}

// PrintAvailableTable prints the table of specs available in the embedded registry.
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

	headers := []string{"NAME", "BACKEND"}
	rows := make([][]string, len(entries))
	for i, r := range entries {
		rows[i] = []string{r.Name, cell(r.Backend)}
	}
	w := colWidths(headers, rows)

	if !IsColorEnabled() {
		fmtStr := fmt.Sprintf("%%-%ds %%-%ds %%s\n", w[0], w[1])
		fmt.Printf(fmtStr, headers[0], headers[1], "REPO")
		fmt.Printf(fmtStr, strings.Repeat("-", w[0]), strings.Repeat("-", w[1]), "----")
		for _, r := range entries {
			fmt.Printf(fmtStr, r.Name, cell(r.Backend), cell(r.Repo))
		}
		return
	}

	header := fmt.Sprintf("%s  %s  %s",
		headerStyle.Width(w[0]).Render(headers[0]),
		headerStyle.Width(w[1]).Render(headers[1]),
		headerStyle.Render("REPO"),
	)
	fmt.Println(header)

	for _, r := range entries {
		row := fmt.Sprintf("%s  %s  %s",
			nameStyle.Width(w[0]).Render(r.Name),
			dimStyle.Width(w[1]).Render(cell(r.Backend)),
			dimStyle.Render(cell(r.Repo)),
		)
		fmt.Println(row)
	}
}

// OutdatedEntry is a row of the "paq outdated" table: an app pinned to
// "latest" whose installed version differs from the resolved upstream one.
type OutdatedEntry struct {
	Name      string `json:"name"`
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
}

// PrintOutdatedTable prints the apps that have a newer upstream version available.
func PrintOutdatedTable(entries []OutdatedEntry) {
	if Global.JSON {
		data, _ := json.MarshalIndent(entries, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(entries) == 0 {
		fmt.Println("All tools are up to date.")
		return
	}

	headers := []string{"APP", "INSTALLED"}
	rows := make([][]string, len(entries))
	for i, e := range entries {
		rows[i] = []string{e.Name, e.Installed}
	}
	w := colWidths(headers, rows)

	if !IsColorEnabled() {
		fmtStr := fmt.Sprintf("%%-%ds %%-%ds %%s\n", w[0], w[1])
		fmt.Printf(fmtStr, headers[0], headers[1], "LATEST")
		fmt.Printf(fmtStr, strings.Repeat("-", w[0]), strings.Repeat("-", w[1]), "------")
		for _, e := range entries {
			fmt.Printf(fmtStr, e.Name, e.Installed, e.Latest)
		}
		return
	}

	header := fmt.Sprintf("%s  %s  %s",
		headerStyle.Width(w[0]).Render(headers[0]),
		headerStyle.Width(w[1]).Render(headers[1]),
		headerStyle.Render("LATEST"),
	)
	fmt.Println(header)

	latestStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // yellow: draws the eye to the new version
	for _, e := range entries {
		row := fmt.Sprintf("%s  %s  %s",
			nameStyle.Width(w[0]).Render(e.Name),
			dimStyle.Width(w[1]).Render(e.Installed),
			latestStyle.Render(e.Latest),
		)
		fmt.Println(row)
	}
}

// PrintConfigShow prints the evaluated user configuration path and its data:
// the effective defaults (configured or built-in) and the declared apps.
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

	// Key in bold blue, value in green, annotations in gray, so labels and
	// values are distinguishable at a glance.
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

	type appRow struct{ use, ver, dest string }
	rowData := make(map[string]appRow, len(keys))
	tableRows := make([][]string, len(keys))
	for i, k := range keys {
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
		rowData[k] = appRow{use, ver, dest}
		tableRows[i] = []string{k, use, ver}
	}

	headers := []string{"APP", "USE", "VERSION"}
	w := colWidths(headers, tableRows)

	if IsColorEnabled() {
		header := fmt.Sprintf("%s  %s  %s  %s",
			headerStyle.Width(w[0]).Render(headers[0]),
			headerStyle.Width(w[1]).Render(headers[1]),
			headerStyle.Width(w[2]).Render(headers[2]),
			headerStyle.Render("DEST"),
		)
		fmt.Println(header)
	} else {
		fmtStr := fmt.Sprintf("%%-%ds %%-%ds %%-%ds %%s\n", w[0], w[1], w[2])
		fmt.Printf(fmtStr, headers[0], headers[1], headers[2], "DEST")
	}
	for _, k := range keys {
		row := rowData[k]
		if IsColorEnabled() {
			fmt.Printf("%s  %s  %s  %s\n",
				nameStyle.Width(w[0]).Render(k),
				dimStyle.Width(w[1]).Render(row.use),
				lipgloss.NewStyle().Width(w[2]).Render(row.ver),
				dimStyle.Render(row.dest),
			)
		} else {
			fmtStr := fmt.Sprintf("%%-%ds %%-%ds %%-%ds %%s\n", w[0], w[1], w[2])
			fmt.Printf(fmtStr, k, row.use, row.ver, row.dest)
		}
	}
}

// PrintInfoDetail prints an app's details (recipe + installed versions).
// installed contains all the app's versions present in the state (can be empty).
func PrintInfoDetail(name string, spec config.Spec, app config.AppEntry, installed []state.InstalledApp) {
	if Global.JSON {
		out := map[string]any{
			"name":      name,
			"spec":      spec,
			"app":       app,
			"installed": installed,
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

// PrintSpecDetail prints the details of a single registry spec.
func PrintSpecDetail(name string, spec config.Spec) {
	if Global.JSON {
		out := map[string]any{
			"name": name,
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
		// An empty label (continuation row) is aligned without the colon.
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

// formatBinaries renders each Binary as "from → to" (or just "from"/"to" when
// one of the two is missing, e.g. a bare download). Returns nil if the list is empty.
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

// colWidths computes, for each column, the width needed to fit both its
// header and every row's cell: max(len(header), max(len(cell))). Used so
// table columns don't truncate or misalign on long values.
func colWidths(headers []string, rows [][]string) []int {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	return widths
}

// sortedPackages returns a copy sorted by name then version.
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
