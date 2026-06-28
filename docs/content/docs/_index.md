---
title: "Documentation"
---

## Install

Build from source:

```bash
git clone https://github.com/enr/paq
cd paq
./.sdlc/build
# Binary: ./bin/paq
```

## Configuration

Create `~/.config/paq/config.toml`:

```toml
[apps.rg]
use = "ripgrep"
version = "latest"
dest = "~/bin/rg{{ext}}"
```

## Commands

| Command | Description |
|---------|-------------|
| `paq install [app]` | Install a tool, or all tools from the manifest |
| `paq upgrade [app]` | Upgrade tools pinned to `latest` to the newest release |
| `paq uninstall <app>` | Uninstall a tool |
| `paq ls` | List installed tools |
| `paq info <app>` | Show definition and install state for a manifest app |
| `paq search <query>` | Search the registry for tool definitions |
| `paq registry list [query]` | List tool definitions in the embedded registry |
| `paq registry show <name>` | Show details of a single registry definition |
| `paq self-update` | Update paq itself to the latest release |

See the [README](https://github.com/enr/paq#readme) for the full reference.
