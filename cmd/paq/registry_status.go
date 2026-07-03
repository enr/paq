package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/enr/paq/embedded"
	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/registry"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var registryStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the state of the registry (embedded and external snapshot)",
	Long:  "Report the embedded registry version, the external snapshot (if installed) and which definitions are overridden by the snapshot or the user manifest.",
	Args:  cobra.NoArgs,
	RunE:  runRegistryStatus,
}

func init() {
	registryCmd.AddCommand(registryStatusCmd)
}

type externalStatus struct {
	Version   string    `json:"version"`
	Tag       string    `json:"tag"`
	SourceURL string    `json:"source_url"`
	FetchedAt time.Time `json:"fetched_at"`
	SpecCount int       `json:"spec_count"`
}

type registryStatus struct {
	EmbeddedVersion      string          `json:"embedded_version"`
	EmbeddedSpecs        int             `json:"embedded_specs"`
	External             *externalStatus `json:"external"`
	ExternalError        string          `json:"external_error,omitempty"`
	ActiveSpecs          int             `json:"active_specs"`
	OverriddenByExternal []string        `json:"overridden_by_external"`
	OverriddenByUser     []string        `json:"overridden_by_user"`
}

func runRegistryStatus(cmd *cobra.Command, args []string) error {
	embeddedSpecs, err := config.LoadEmbeddedRegistry(embedded.RegistryFS)
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Open the snapshot directly to distinguish absent from corrupt.
	_, meta, openErr := registry.Open()

	var byExt, byUser []string
	for name, spec := range cfg.Specs {
		switch spec.Origin {
		case config.OriginRegistry:
			if _, ok := embeddedSpecs[name]; ok {
				byExt = append(byExt, name)
			}
		case config.OriginUser:
			byUser = append(byUser, name)
		}
	}
	sort.Strings(byExt)
	sort.Strings(byUser)

	st := registryStatus{
		EmbeddedVersion:      Version,
		EmbeddedSpecs:        len(embeddedSpecs),
		ActiveSpecs:          len(cfg.Specs),
		OverriddenByExternal: byExt,
		OverriddenByUser:     byUser,
	}
	if openErr != nil {
		st.ExternalError = openErr.Error()
	} else if meta != nil {
		st.External = &externalStatus{
			Version:   meta.Version,
			Tag:       meta.Tag,
			SourceURL: meta.SourceURL,
			FetchedAt: meta.FetchedAt,
			SpecCount: meta.SpecCount,
		}
	}

	if ui.Global.JSON {
		data, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	ui.OKField("Embedded registry", fmt.Sprintf("%s (%d recipes)", Version, len(embeddedSpecs)))
	switch {
	case openErr != nil:
		ui.WarnField("External registry", "unusable", "("+openErr.Error()+")")
		ui.Hint("run `paq registry update` to refresh the external registry")
	case meta == nil:
		ui.OKField("External registry", "not installed")
		ui.Hint("run `paq registry update` to download the latest registry")
	default:
		ui.OKField("External registry", fmt.Sprintf("%s (%d recipes)", meta.Version, meta.SpecCount))
		if src := metaSource(meta); src != "" {
			ui.OKField("  source", src)
		}
		ui.OKField("  fetched", fmt.Sprintf("%s (%s)", meta.FetchedAt.Local().Format("2006-01-02 15:04"), humanAge(meta.FetchedAt)))
		if len(byExt) > 0 {
			ui.OKField("  overrides embedded", strings.Join(byExt, ", "))
		}
	}
	ui.OKField("Active recipes", fmt.Sprintf("%d", len(cfg.Specs)))
	if len(byUser) > 0 {
		ui.OKField("Overridden by user", strings.Join(byUser, ", "))
	}
	return nil
}

// metaSource describes where the snapshot came from.
func metaSource(m *registry.Meta) string {
	switch {
	case m.Tag != "" && m.Tag != "custom":
		return m.Tag
	case m.SourceURL != "":
		return m.SourceURL
	default:
		return ""
	}
}

// humanAge renders a coarse "N days/hours ago" for a fetch timestamp.
func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}
