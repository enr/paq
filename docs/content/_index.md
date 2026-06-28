---
title: "paq"
---

`paq` installs and manages CLI tools from GitHub releases and direct URLs,
driven by a simple TOML configuration.

```bash
# Install a tool from the registry
paq install rg

# List tool definitions available in the embedded registry
paq registry list

# Show details of a single definition
paq registry show ripgrep
```

Pre-built binaries for `linux`, `darwin`, and `windows` on `amd64` / `arm64`.
