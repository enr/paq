# paq — Feature / documentation / test audit

Snapshot of `main` at `e5a4561`, version `0.0.16-SNAPSHOT`.

Scope: every user-facing command and every configuration field the TOML format
accepts, checked against the two documentation surfaces (`README.md` and the
Hugo site under `docs/content/`) and against the test suite
(`go test ./...`, plus the gated `e2e` suite).

Measurements come from `go test ./... -cover` and `go tool cover -func`.

---

## 1. Command inventory

19 runnable commands plus one hidden internal command.

| Command | Aliases | README | Site | `Long:` help | Coverage of `runX` | Dedicated test file |
|---|---|---|---|---|---|---|
| `install [app...]` | `i` | yes | yes | **no** | 24.2% | `install_test.go` (8) |
| `uninstall <app...>` | `rm`, `remove` | yes | yes | **no** | 71.9% | `uninstall_test.go` (9) |
| `upgrade [app...]` | `up`, `u` | yes | yes | yes | 54.5% | `upgrade_test.go` (8) |
| `ls` | `list` | yes | yes | **no** | 62.5% | **none** |
| `info <app>` | — | yes | yes | **no** | **0.0%** | **none** |
| `which <app>` | — | yes | yes | yes | 83.3% | `which_test.go` (5) |
| `outdated` | — | yes | yes | yes | **0.0%** | `outdated_test.go` (5, helpers only) |
| `search <query>` | `s` | yes | yes | yes | via `listDefinitions` | **none** |
| `import <spec>` | — | yes | yes | yes | **0.0%** | `import_test.go` (3, helpers only) |
| `init` | — | yes | yes | yes | 68.4% | `init_test.go` (3) |
| `doctor` | — | yes | yes | yes | 53.8% | **none** |
| `version` | — | yes | yes | **no** | — | `version_test.go` (1) |
| `completion <shell>` | — | yes | yes | — | — | `completion_test.go` (4) |
| `self-update` | — | yes | yes | yes | **0.0%** | `self_update_test.go` (6, helpers only) |
| `config` | — | yes | yes | yes | — | — |
| `config show` | — | yes | yes | yes | **0.0%** | **none** |
| `registry` | `reg` | yes | yes | yes | — | — |
| `registry list [query]` | `ls` | yes | yes | yes | 75.0% | **none** |
| `registry show <name>` | — | yes | yes | yes | **0.0%** | **none** |
| `registry status` | — | yes | yes | yes | 45.7% | **none** |
| `registry update` | — | yes | yes | yes | — | `registry_update_test.go` (10) |
| `__update-check` (hidden) | — | n/a | n/a | n/a | — | `update_notify_test.go` (4) |

**Command-level documentation is complete**: all 19 public commands appear in
both the README and the site. The gaps are in *flags* and *config fields*
(§2, §3).

### Flags

Global: `--no-color`, `-j/--json`, `-q/--quiet`, `-v/--verbose`, `--debug` —
all documented in both surfaces.

Per-command flags, and where they are documented:

| Flag | Command | README | Site |
|---|---|---|---|
| `--force`, `-f` | `install`, `init`, `import`, `registry update`, `self-update` | **no** | yes |
| `--no-save` | `install` | yes | yes |
| `--dry-run` | `uninstall` | **no** | yes |
| `--yes`, `-y` | `uninstall` | **no** | yes |
| `--as`, `-a` | `import` | yes | yes |
| `--dest` | `import` | yes | yes |
| `--version` | `import` | **no** | yes |
| `--write`, `-w` | `import` | yes | yes |
| `--check`, `-c` | `self-update` | yes | yes |
| `--fix` | `doctor` | yes | yes |

---

## 2. Configuration-field inventory

Every field the TOML decoder accepts (`internal/config/types.go`), against the
docs and against actual use in the shipped registry (`embedded/registry/*.toml`).

| Field | README | Site | Used by shipped recipes |
|---|---|---|---|
| `backend`, `repo`, `asset`, `source`, `archive`, `extract`, `chmod`, `strip_components` | yes | yes | yes |
| `binaries` (`from`/`to`) | yes | yes | yes |
| `latest_strategy`, `arch_pkg`, `latest_url`, `latest_json` | yes | yes | yes |
| `default_version` | yes | yes | yes |
| `os`, `arch`, `env` | yes | yes | yes |
| `verify.sha256`, `sha256_asset`, `sha256_url`, `sha256_json` | yes | yes | yes |
| `[registry] url` / `public_key` | yes | yes | n/a |
| `[defaults] bin` / `opt` / `check_updates` | yes | yes | n/a |
| `apps.*` (`use`, `version`, `dest`) | yes | yes | n/a |
| `tag` | **no** | yes | `bun`, `jq`, `ripgrep`, `temurin` |
| `subdir` | **no** | yes | `jvm` |
| `env_arch` | **no** | yes | `ripgrep` |
| `minimum_release_age` (spec + `[defaults]`) | **no** | yes | — |
| per-OS override blocks (`[specs.x.windows]`) | **no** | yes | yes |
| **`platforms`** | **no** | **no** | `bun`, `gip`, `inner`, `micro`, `runp`, `vscode` |
| **`templates`** | **no** | **no** | `templates`, `vscode` |
| **`templates_os`** | **no** | **no** | `vscode` |
| **`verify.sha512`** | **no** | **no** | `jvm` |
| **`verify.sha512_asset`** | **no** | **no** | `jvm` |
| **`verify.minisign.public_key` / `signed_asset`** (spec-level) | **no** | **no** | — |

Six fields are implemented, exercised by the registry that ships inside the
binary, and documented **nowhere**.

---

## 3. Package-level test coverage

| Package | Coverage | Note |
|---|---|---|
| `internal/template` | 91.1% | |
| `internal/pathenv` | 90.0% | Windows path never executed on CI |
| `internal/verify` | 88.7% | |
| `internal/httpretry` | 87.5% | |
| `internal/platform` | 83.3% | |
| `internal/version` | 83.2% | `Compare`/`compareNumeric` at 0% |
| `internal/state` | 82.8% | |
| `internal/backend` | 82.3% | `url.go Resolve` at 0% |
| `internal/config` | 82.1% | |
| `internal/install` | 77.3% | |
| `internal/download` | 74.0% | |
| `internal/archive` | 72.3% | `tar.xz` path at 0% |
| `internal/registry` | 66.7% | |
| `internal/updatecheck` | 57.5% | |
| `cmd/paq` | 50.7% | |
| **`internal/ui`** | **2.1%** | one test (`TestColWidths`) |
| **`internal/jsonpath`** | **0.0%** | **no test file** |

`e2e/`: a single test (`TestInstallRipgrep`), gated behind `workflow_dispatch`,
covering the `github` backend + `tar.gz` + single `extract` only.

---

## 4. Gap report

### HIGH

**H1 — The README states a security guarantee the code does not provide.**
`README.md:323-328` says the registry checksum is minisign-signed, that "paq
embeds the public key as the trust anchor", and that "the signature is the
security boundary — **it is never optional**". The code says otherwise:
`internal/registry/trust.go:17` ships `DefaultPublicKey = ""`, and an empty key
"falls back to checksum-only verification with a warning". The site
(`docs/content/docs/_index.md:647-650`) documents this correctly — the README
does not. A user reading the README believes downloads from the default source
are signature-verified when they are not. Fix the README to match the site.

**H2 — `internal/jsonpath` has no tests at all, and sits on the checksum path.**
`Select`/`String` resolve `verify.sha256_json` (which hash is checked) and
`latest_json` (which version is installed). Zero coverage on the component that
decides *which bytes count as the expected hash* is the riskiest hole in the
suite: a selector that silently resolves to the wrong node yields a verification
that passes against an attacker-chosen digest. Untested branches include array
indexing, out-of-range indices, missing keys, and the non-string assertion.

**H3 — `version.Compare` is untested and drives four decisions.**
`internal/version/clean.go:40` is at 0% coverage, yet it decides whether
`self-update` replaces the binary (`self_update.go:73`), whether
`registry update` overwrites a snapshot (`registry_update.go:151`), whether a
snapshot is flagged stale (`version.go:48`), and whether the daily update hint
fires (`update_notify.go:86`). It also compares only major/minor/patch:
build metadata (`Build()` exists but is not consulted) and any fourth component
are dropped, so `21.0.2+13` and `21.0.2+9` compare equal. Whether that is
intended is undocumented and unasserted.

**H4 — Six shipped config fields are documented nowhere.**
`platforms`, `templates`, `templates_os`, `verify.sha512`,
`verify.sha512_asset` and spec-level `verify.minisign.*` are absent from both
the README and the site. `platforms` alone gates installability in six shipped
recipes; `templates`/`templates_os` define custom placeholders used by `vscode`.
A user reading a shipped recipe cannot understand it, and cannot write an
equivalent one. `sha512` is a verification primitive — undocumented verification
options tend to go unused.

**H5 — The `tar.xz` extraction path has zero coverage.**
`internal/archive/archive_test.go` is the strongest file in the suite: 22 tests
covering path traversal, absolute and escaping symlink targets, symlink chains,
hardlinks outside the extract scope, and metadata entries. All of them exercise
`tar.gz` and `zip`. `extractTarXz` (`internal/archive/tarxz.go:10`) is at 0%, so
none of that hardening is proven for the third supported format.

### MEDIUM

**M1 — `minimum_release_age` is missing from the README.**
A supply-chain guard (don't install a release younger than N) added in
`124ac7f`, configurable per spec and under `[defaults]`, documented on the site
and not at all in the README.

**M2 — Five flags are missing from the README.** `--force` (five commands),
`--dry-run` and `--yes` (`uninstall`), `--version` (`import`). `--yes` matters
most: it is *required* in non-interactive sessions, so a README-only reader
scripting `paq uninstall` hits a hang or a failure with no documented remedy.

**M3 — Six command entry points are at 0% coverage.** `runConfigShow`,
`runInfo`, `runImport`, `runOutdated`, `runRegistryShow`, `runSelfUpdate`.
`import_test.go`, `outdated_test.go` and `self_update_test.go` exist but test
helpers only — the command wiring (flag handling, arg validation, error paths,
JSON vs table branch) is never entered. `config show`, `info`, `ls`, `search`,
`doctor`, `registry list/show/status` have no test file at all.

**M4 — `internal/ui` is effectively untested (2.1%).** All 579 lines of
`table.go` — `PrintLsTable`, `PrintAvailableTable`, `PrintOutdatedTable`,
`PrintConfigShow`, `PrintInfoDetail`, `PrintSpecDetail` — are at 0%, as is all
of `log.go`. This is the entire user-visible output surface. Combined with M3,
the `--json` output shape of `config show`, `info`, `outdated` and
`registry show` is unverified, despite being the documented contract for
scripting (`README.md:185`).

**M5 — CI tests one platform; three files are Windows-only.**
`.github/workflows/ci.yml` runs `./.sdlc/test` on `ubuntu-latest` only.
`internal/pathenv/pathenv_windows.go`, `internal/state/process_windows.go` and
`cmd/paq/update_notify_windows.go` are cross-*compiled* by the `cross-build` job
but never executed, and `doctor --fix` is documented as Windows-only. macOS is
likewise never tested. Six platforms are published; one is tested.

**M6 — The e2e suite covers one shape of one backend.** One test, `github` +
`tar.gz` + single-file `extract`. Not covered end to end: the `url` backend,
`zip`, `tar.xz`, `binaries[]` (multi-executable and bare-binary forms),
`strip_components`/`subdir`, side-by-side multi-version installs, and
`uninstall`. The job is also `workflow_dispatch`-gated, so it does not run on
pull requests — a regression in the real download path reaches `main` unseen.

**M7 — `internal/backend/url.go:13 Resolve` is at 0%.** One of the two
backends; its resolution path is exercised only indirectly, if at all.

**M8 — Five commands have no `Long:` help text.** `install`, `uninstall`,
`ls`, `info`, `version` — including the two commands that carry the most
semantics (`install`'s manifest-recording behaviour, `uninstall`'s `app@version`
disambiguation). `paq help install` tells the user materially less than the
README does.

### LOW

**L1 — README and site have drifted.** The site is a strict superset on every
axis measured here (flags, fields, per-OS overrides, `minimum_release_age`, the
signature caveat). Nothing declares which is canonical, so both are maintained
by hand and the README loses. Consider generating the command/flag reference
from cobra, or making the README a short pointer to the site.

**L2 — `tag` and `subdir` are missing from the README** (documented on the
site; `tag` is used by four shipped recipes).

**L3 — Stale references.** `.sdlc/e2e:2` describes itself as "End-to-end
integration tests for **grab**" — the project is `paq`.
`internal/registry/trust.go:15` points at `plan/registry-signing-enablement.md`;
there is no `plan/` directory in the repository.

**L4 — Minor 0%-coverage helpers**, listed for completeness rather than as
risks: `config.UserManifestPath`, `download.NewClient`, `ui.IsTTY`,
`ui.IsColorEnabled`, `install.pipeline.Error`/`Unwrap`,
`cmd/paq` `completeManifestApps`/`completeInstallableNames`, `reportError`,
`humanAge`, `metaSource`, `printUninstallTargets`, `cleanupOldVersions`,
`appHooks`, `installParallel`, `replaceExecutable`, `spawnDetached`.

---

## 5. What is in good shape

Worth recording so the gaps above are read in proportion:

- **Command documentation is complete.** All 19 public commands are documented
  on both surfaces, with examples.
- **Archive extraction security is tested seriously.** 22 tests covering path
  traversal, symlink escape, symlink chains and hardlink scope — for the two
  formats it covers.
- **`internal/verify` at 88.7%**, with dedicated tests per algorithm and for the
  JSON-document and minisign paths.
- **The test runner is well set up.** `.sdlc/test` runs `-shuffle=on -race`,
  which catches order dependence and races in the parallel install path.
- **Nine packages sit above 80%.**

---

## 6. Suggested order of work

1. H1 — one README edit; it is the only item that misleads about security.
2. H2, H3 — two small unit-test files (`jsonpath`, `version.Compare`); highest
   risk-reduction per line of test written.
3. H4 — document the six missing fields, on the site first, then the README.
4. H5 — a `tar.xz` fixture through the existing archive test table.
5. M6 — ungate e2e on pull requests, then widen it (`url` backend, `zip`,
   `binaries[]`).
6. M1, M2, L2 — one README pass closing every remaining flag/field gap.
7. M3, M4 — golden-output tests for the table and `--json` renderers; these
   close two gaps at once.
8. M5 — add `windows-latest` and `macos-latest` to the CI test matrix.
