# paq

`paq` installs and manages CLI tools from GitHub releases and direct URLs, driven by a simple TOML configuration.

paq is built for one job: **installing tools and applications, fast**, without
touching the rest of your environment. It drops binaries into plain directories
you own — no daemon, no runtime, no shell hooks — so it stays out of your way
and is never invasive.

It's meant to run **alongside** your system's official package manager, not to
replace it. Reach for paq when a tool isn't in the official repositories yet,
when you want a bleeding-edge release ahead of what your distro ships, or when
you need several versions of the same tool at once.

It is not a version manager and won't switch "active" versions for you. What it
*does* make effortless is keeping several versions of the same tool side by
side, each in its own destination, nothing else disturbed.

**Documentation: <https://enr.github.io/paq/docs/>**

## Install

Download the zip for your platform from the
[releases page](https://github.com/enr/paq/releases) (`linux`, `darwin` and
`windows`, each on `amd64` and `arm64`; every release ships a `SHA256SUMS`
file), unzip it and put `paq` on your `PATH`. Then keep it current with
`paq self-update`, and run `paq doctor` to check the setup.

Or build from source:

```bash
git clone https://github.com/enr/paq
cd paq
./.sdlc/build
# Binary: ./bin/paq
```

## Quickstart

For any tool in the embedded registry, no config file is needed:

```bash
paq search jdk           # browse the registry
paq install ripgrep      # install it and record it in the manifest
paq install bat delta    # several at once
paq ls                   # what is installed
paq upgrade              # move "latest"-tracking tools to the newest release
```

To customise versions and destinations, write `~/.config/paq/config.toml`:

```toml
[apps.rg]
use = "ripgrep"
version = "latest"
dest = "~/bin/rg{{ext}}"

[apps.jdk]
use = "jdk"
version = "21"
dest = "~/opt/jdk-{{version}}"
```

then run `paq install` to install everything it declares.

## Highlights

- **Verified downloads**: SHA-256/SHA-512 checksums, minisign and cosign
  signatures, GitHub build-provenance attestations; `--require-verify` refuses
  recipes with none.
- **Reproducible installs**: `paq.lock.toml` pins what "latest" resolved to,
  and each platform's archive hash, for every machine sharing the manifest.
- **Several versions side by side**, through distinct manifest entries or a
  one-off `paq install tool@x.y.z --no-save --dest ...`.
- **Project-local installs**: `paq --local install` reads the project's
  `paq.toml` and keeps the tools, and their install state, under `.paq/`.
- **Your own recipes** in the manifest, overriding the embedded ones, plus an
  external registry snapshot updated independently of the binary.
- **CI-friendly**: `--json` output, distinct exit codes, an opt-in download
  cache (`PAQ_DOWNLOAD_CACHE`), musl (Alpine) support.

## Where to go next

- [Configuration](https://enr.github.io/paq/docs/#configuration): manifest,
  default directories, `version` modes, lockfile, download cache
- [Project-local installs](https://enr.github.io/paq/docs/#project-local-installs---local):
  per-project tools with `--local`
- [Commands](https://enr.github.io/paq/docs/#commands): full reference, flags
  and exit codes
- [Adding a custom recipe](https://enr.github.io/paq/docs/#adding-a-custom-recipe):
  recipe fields, verification, platforms, templates, resolving `latest`
- [External registry](https://enr.github.io/paq/docs/#external-registry):
  snapshots, verification and custom sources

The documentation sources live in [`docs/content/docs/_index.md`](docs/content/docs/_index.md).
