package embedded

import "embed"

//go:embed registry/*.toml
var RegistryFS embed.FS
