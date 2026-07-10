---
title: "paq"
---

Grab a tool from the embedded registry, or drive everything from a TOML
manifest — no runtime, no daemon, one static binary.

paq runs **alongside** your system's package manager: reach for it when a tool
isn't in the official repos yet, when you want a bleeding-edge release, or to
keep several versions of the same tool side by side — without touching your
shell, profile, or system directories.

```bash
# Install a tool from the registry
paq install rg

# List tool definitions available in the embedded registry
paq registry list

# Show details of a single definition
paq registry show ripgrep
```

Then head to the [documentation](docs/) for the manifest format, custom recipes,
and the full command reference.
