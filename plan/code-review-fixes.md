# Implementation plan: code review findings

Source: full-codebase review of 2026-07-04. The two High findings (minisign-only
verification silently skipped; empty registry trust anchor) are already fixed on
this branch; note that registry signature enforcement is temporarily optional —
the rollout to mandatory signing is a separate plan, see
`plan/registry-signing-enablement.md`. This document is the implementation plan
for the remaining Medium and Low findings. Each item is self-contained: it names the exact files,
functions and behavior changes, the tests to add, and the acceptance criteria.
Items are independent unless a dependency is stated; each should land as its
own commit.

Conventions used below:

- "spec" = `config.Spec`, "app" = `config.AppEntry`.
- Line numbers refer to the branch state at the time of writing; use the quoted
  code as the anchor, not the number.

---

## M1. Apply the `env` mapping (or remove the field) — DONE

**Problem.** `Spec.Env` and `AppEntry.Env` (`internal/config/types.go`) are
decoded from TOML and rendered by `paq registry show`
(`internal/ui/table.go`, `renderMap("Env", spec.Env)`), but the install
pipeline never applies them: `internal/install/pipeline.go` calls
`platform.ApplyMap` only for OS and Arch. A spec with `[x.env]` has no effect
on `{{env}}`.

**Decision.** Apply the mapping. The field is public API (documented by
`registry show`), and the mapping is useful (e.g. `gnu` → `musl` per spec).

**Changes.**

1. `internal/install/pipeline.go`, in the block that computes `resolvedArch` /
   `resolvedOS` (after `spec = spec.ApplyOSOverride(plat.OS)`), add the same
   two-level resolution for env:

   ```go
   resolvedEnv := platform.ApplyMap(spec.Env, plat.Env, plat.Env)
   if app.Env != nil {
       resolvedEnv = platform.ApplyMap(app.Env, plat.Env, resolvedEnv)
   }
   ```

   and use `Env: resolvedEnv` (instead of `plat.Env`) when building
   `template.Vars`. Keys of the map are the canonical values (`plat.Env`,
   i.e. `"gnu"` on linux, `""` elsewhere), mirroring how `spec.OS`/`spec.Arch`
   are keyed by canonical OS/arch.

2. Update the `dbg("resolved: ...")` line to include `env=%q`.

**Tests.** In `internal/install/pipeline_test.go`, add a url-backend test with
`Spec.Env: map[string]string{"gnu": "musl"}` and `Source` containing
`{{env}}`; assert the request path received by the `httptest` server contains
`musl`. Add a second case where `AppEntry.Env` overrides the spec value.

**Acceptance.** `[x.env]` and `[apps.x.env]` change `{{env}}` in asset/source
templates; existing specs (none set `env`) are unaffected; full test suite
green.

---

## M2. Key per-OS meta-template overrides by canonical OS — DONE

**Problem.** `internal/install/pipeline.go` builds `template.Vars` with
`OS: resolvedOS` (the *mapped* OS, e.g. `"mac"` for temurin) and then
`template.Expand` (`internal/template/metatemplate.go`) looks up the per-OS
override map with `osOverrides[v.OS]`. The override tables
(`[templates.darwin]` in `templates.toml`, `[x.templates_os.darwin]` in specs)
are keyed by canonical OS names, so any spec that maps the OS silently loses
its per-OS meta-templates.

**Changes.**

1. Change the signature of `template.Expand` to take the canonical OS
   explicitly:

   ```go
   func Expand(mt MetaTemplates, osOverrides map[string]MetaTemplates, canonicalOS string, v Vars) (Vars, error)
   ```

   and use `osOverrides[canonicalOS]` for the lookup. Do not read `v.OS` for
   the lookup anymore (it stays the value substituted into `{{os}}`).

2. In `pipeline.go`, call `template.Expand(globalMT, osMT, plat.OS, vars)`.

3. Update `internal/template/template_test.go` callers of `Expand`.

**Ordering note.** Today `Expand` runs *after* the OS/arch mapping, so
meta-templates already see the mapped `{{os}}` — that behavior is correct and
must not change (rust_target needs the mapped values). Only the *override
selection* key changes.

**Tests.** In `template_test.go`: an override map keyed `"darwin"`, vars with
`OS: "mac"`, `canonicalOS: "darwin"` → the darwin override applies. Second
case: `canonicalOS: "linux"` → the global template applies.

**Acceptance.** A spec combining `[x.os] darwin = "mac"` with a
`[templates.darwin]` override resolves the darwin override; suite green.

---

## M3. Make meta-template expansion deterministic — DONE

**Problem.** `template.Expand` iterates `MetaTemplates` (a Go map) in random
order while writing results into `v.Extra`, which later iterations can read.
A meta-template referencing another meta-template resolves or fails depending
on iteration order.

**Decision.** Reject references between meta-templates (simplest; there is
exactly one meta-template today and no dependent ones). Do NOT implement
topological resolution — it is complexity nobody asked for.

**Changes.**

1. In `template.Expand`, expand every template against a *copy* of the vars
   that has the incoming `v.Extra` but never the values produced during this
   same Expand call:

   ```go
   base := v          // snapshot; base.Extra is the incoming Extra map
   for k, tmpl := range mt {
       val, err := Resolve(tmpl, base)   // resolves against snapshot only
       ...
       v.Extra[k] = val
   }
   ```

   Note `Vars` is a value type but `Extra` is a shared map: `base` must get
   its own copy of the incoming Extra map (`maps.Clone`) taken before the
   loop, so writes to `v.Extra` are invisible to `Resolve`.

2. Apply the same snapshot rule to the per-OS override loop (snapshot taken
   once, before the global loop: overrides may not reference globals either —
   they are alternatives to them, not consumers).

3. Document in the `Expand` doc comment: "Meta-templates cannot reference
   other meta-templates; such a reference fails with unknown placeholder."

**Tests.** A `MetaTemplates{"a": "{{arch}}", "b": "{{a}}"}` map must
deterministically fail with `unknown placeholder "a"` (run the expansion in a
loop ~50 times in the test to catch order dependence).

**Acceptance.** Same input always produces the same output/error; suite green.

---

## M4. One shared definition of "tracks latest" for install/upgrade/outdated — DONE

**Problem.** The pipeline treats `version = ""` as "default_version, else
latest" (`internal/install/pipeline.go`, the `switch` on `app.Version`), but
`cmd/paq/upgrade.go` (`strings.ToLower(app.Version) != "latest"`) and
`cmd/paq/outdated.go` (`!strings.EqualFold(app.Version, "latest")`) treat only
the literal `"latest"` as upgradeable. An app without `version` whose spec has
no `default_version` installs the newest release but is skipped forever by
upgrade/outdated, with the broken message `pinned to , skipping`.

**Changes.**

1. Add to `internal/config/types.go`:

   ```go
   // TracksLatest reports whether the app entry follows the newest upstream
   // version: an explicit "latest", or an omitted version when the spec has
   // no default_version to pin to. Must stay in sync with the pipeline's
   // version-resolution switch.
   func (a AppEntry) TracksLatest(spec Spec) bool {
       if strings.EqualFold(a.Version, "latest") {
           return true
       }
       return a.Version == "" && spec.DefaultVersion == ""
   }
   ```

2. `cmd/paq/upgrade.go` (`upgradeApp`): replace the string check with
   `app.TracksLatest(spec)`. This requires moving the spec lookup (already
   present a few lines below) *above* the check. Fix the skip message for the
   empty-version-with-default case: `step("pinned to %s, skipping", ...)` must
   print `spec.DefaultVersion` when `app.Version == ""` (never an empty
   string).

3. `cmd/paq/outdated.go` (`checkOutdated`): same substitution; the spec lookup
   also moves above the check. Same message fix.

4. `internal/install/pipeline.go`: add a comment on the version-resolution
   switch pointing at `AppEntry.TracksLatest` ("keep in sync").

**Tests.**

- `cmd/paq/upgrade_test.go`: manifest entry with empty version + spec without
  `default_version` + fake github release server → upgrade resolves and
  reinstalls (not skipped).
- Entry with empty version + spec *with* `default_version` → skipped with
  message containing the default version, not `pinned to ,`.
- `config/types_test.go`: table test for `TracksLatest` (4 combinations).

**Acceptance.** `paq install` / `paq upgrade` / `paq outdated` agree on
which entries track latest; no message ever prints an empty version; suite
green.

---

## M5. `buildAuxURL` must fail instead of producing a wrong URL — DONE

**Problem.** `internal/install/pipeline.go`:

```go
func buildAuxURL(downloadURL, assetName, auxName string) string {
    base := strings.TrimSuffix(downloadURL, assetName)
    return base + auxName
}
```

When `downloadURL` does not end with `assetName` (query string; `spec.Asset`
template resolving to something other than the URL basename), `TrimSuffix` is
a no-op and the result is `downloadURL + auxName` — a silently wrong checksum
or signature URL.

**Changes.**

1. Change the signature to return an error:

   ```go
   func buildAuxURL(downloadURL, assetName, auxName string) (string, error) {
       if !strings.HasSuffix(downloadURL, assetName) {
           return "", fmt.Errorf("cannot derive %q: download URL %q does not end with asset name %q", auxName, downloadURL, assetName)
       }
       return strings.TrimSuffix(downloadURL, assetName) + auxName, nil
   }
   ```

2. Propagate the error in `resolveAuxURL` (the closure in `Run`), which
   already returns `(string, error)`.

**Tests.** Unit-test `buildAuxURL` directly (happy path; URL with query
string → error; mismatched asset name → error). Existing url-backend pipeline
tests cover the happy path end-to-end.

**Acceptance.** A mismatch fails with an explicit error naming both URL and
asset name instead of downloading a bogus URL; suite green.

---

## M6. Restrict bare-hash checksum parsing to single-line files — DONE

**Problem.** `internal/verify/sha256.go` (`ParseSHA256File`) and
`internal/verify/sha512.go` (`ParseSHA512File`): the first one-column line
encountered is returned as *the* hash, regardless of which file it belongs to.
A stray one-field line in a multi-file checksum list short-circuits filename
matching, and a bare-hash file for a different artifact is accepted silently.

**Changes.** Rework both parsers identically (they are near-twins; keep them
twins):

1. Read all non-empty, non-`#` lines first.
2. If there is exactly **one** such line and it has exactly **one** field:
   bare-hash mode — validate it (`len == 64` for sha256, `128` for sha512, all
   hex; reject otherwise with `"malformed checksum file %s"`), return it.
3. Otherwise every line must have ≥ 2 fields; match on
   `filepath.Base(strings.TrimPrefix(parts[1], "*")) == wantBase`. A line with
   a single field in a multi-line file is skipped (tolerate trailing garbage)
   — it is no longer a match candidate.
4. Keep the existing "not found" error.

**Tests.** Extend `sha256_test.go` / `sha512_test.go`:

- single bare-hash line → returned (existing behavior, keep green);
- multi-line file where a one-field line precedes the correct
  `hash  filename` line → the named hash is returned, not the bare one;
- single one-field line that is not valid hex/length → error;
- multi-line file without the wanted name → "not found" error.

**Acceptance.** Bare-hash mode only ever applies to genuinely single-entry
files; suite green. Verify manually that the two embedded consumers of
bare-hash files (Maven-style sha512) still install.

---

## M7. Enforce the registry size cap during download

**Problem.** `cmd/paq/registry_update.go` checks `registryMaxBytes` only
after `download.ToTemp` has streamed the whole body to disk. A hostile custom
`[registry].url` can fill the disk before the check runs.

**Changes.**

1. Add a bounded variant in `internal/download/download.go`:

   ```go
   // ToTempLimited is ToTemp with a hard cap on the response size. It fails
   // as soon as the body exceeds maxBytes, and rejects upfront a
   // Content-Length larger than maxBytes.
   func ToTempLimited(ctx context.Context, client *http.Client, url string, maxBytes int64, progress ProgressFn) (string, error)
   ```

   Implementation: factor the body of `ToTemp` into an unexported
   `toTemp(ctx, client, url, maxBytes, progress)`; `ToTemp` calls it with
   `maxBytes = 0` (no limit). Inside: if `maxBytes > 0 &&
   resp.ContentLength > maxBytes` → error before reading; wrap the reader with
   `io.LimitReader(src, maxBytes+1)` and, after `io.Copy`, error if the number
   of copied bytes is `> maxBytes` (`"response exceeds %d bytes"`). The temp
   file is removed on every error path, as today.

2. In `runRegistryUpdate`: download the tarball with
   `download.ToTempLimited(ctx, client, src.tarURL, registryMaxBytes, ...)`;
   delete the post-hoc `os.Stat` size check. Use the same bounded call for the
   checksums and signature files (they are tiny; a generous shared cap of
   `registryMaxBytes` is fine — do not introduce a second knob).

**Tests.** `registry_update_test.go` already lowers `registryMaxBytes`; adapt
the oversized-archive test to assert the download itself fails (error mentions
the byte limit) and that no `.tmp` files remain in `os.TempDir()` scoped to
the test. Unit-test `ToTempLimited` in `download` with an `httptest` server:
body larger than the cap with and without Content-Length.

**Acceptance.** No more than `registryMaxBytes + 1` bytes of a hostile
registry response are ever written to disk; suite green.

---

## M8. tar: extract only regular files, dirs and symlinks — DONE

**Problem.** `internal/archive/tar.go`, in both the Subdir and standard
branches, the `default:` case of the `switch hdr.Typeflag` writes *any*
unknown entry type as a regular file: `TypeXGlobalHeader` (returned by Go's
tar reader → a literal `pax_global_header` file when `strip_components = 0`),
char/block devices, FIFOs.

**Changes.**

1. At the top of the entry loop, immediately after the `TypeLink` rejection,
   skip metadata and special entries explicitly:

   ```go
   switch hdr.Typeflag {
   case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
       continue // metadata entries, never materialized
   case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
       continue // special files: never useful in a tool archive, skip
   }
   ```

2. In the two inner switches, replace `default:` with
   `case tar.TypeReg:` for the `writeFile` arm and add a final `default:
   continue` (belt and braces for exotic typeflags). The Extract
   (single-file) branch keeps its existing
   `hdr.Typeflag != tar.TypeSymlink` guard but gains the same top-level skip
   for free.

**Tests.** In `internal/archive/archive_test.go`, build a tar containing a
`pax_global_header` entry with `Typeflag: tar.TypeXGlobalHeader`, a FIFO
entry, and a regular file; extract with `StripComponents: 0`; assert only the
regular file exists in dest.

**Acceptance.** Extracted trees contain only files, directories and vetted
symlinks; `git archive`-style tarballs no longer produce `pax_global_header`;
suite green.

---

## M9. Refuse to replace or remove a dest that paq does not own

**Problem.** Two sides of the same trust gap:

- `internal/install/dir.go` (`InstallDir`) swaps out whatever exists at
  `dest` and deletes the backup on success. A manifest typo
  (`dest = "~/.local"`) destroys the user's directory.
- `cmd/paq/uninstall.go` (`removeRecordFiles`) runs `os.RemoveAll(rec.Dest)`
  trusting the state blindly.

**Changes.**

1. New helper in `internal/install/pipeline.go` (unexported):

   ```go
   // destOwnedByPaq reports whether dest is recorded in the state DB as the
   // destination of any installed app (any version).
   func destOwnedByPaq(st *state.State, dest string) bool
   ```

   Implementation: linear scan of `st.Packages` comparing
   `filepath.Clean(rec.Dest) == filepath.Clean(dest)`.

2. In `Run`, in the `kind = "dir"` arm only, before calling `InstallDir`:
   if `dest` exists on disk (`os.Stat`), is a **non-empty directory**, and
   `!hooks.Force`, and `!destOwnedByPaq(...)` (load state once via
   `state.Load()`; on load error treat as not-owned), fail with:

   ```
   destination %s already exists and was not created by paq: remove it or use --force
   ```

   Rationale for scoping to "dir": file and binaries installs overwrite single
   named files, an inherently bounded blast radius; only the dir kind removes
   whole trees. `--force` remains the escape hatch and preserves the
   "reinstall over an old tree" flow for the (already owned) common case,
   which is unaffected because the previous install recorded the dest.

3. Uninstall side, `cmd/paq/uninstall.go` `removeRecordFiles`, `"dir"` case:
   refuse blast-radius mistakes cheaply — before `os.RemoveAll`, `os.Stat`
   the dest; if it is not a directory, fall back to `os.Remove`. Additionally
   guard against catastrophic dests: resolve `expandHome`-style values at
   record time (they already are absolute in state) and refuse to remove a
   dest that equals the user home dir or a filesystem root:

   ```go
   if clean := filepath.Clean(rec.Dest); clean == home || clean == filepath.Dir(clean) {
       return fmt.Errorf("refusing to remove %s: not a paq-managed directory", rec.Dest)
   }
   ```

   (`clean == filepath.Dir(clean)` is true exactly for roots, on both Unix
   and Windows.)

**Tests.**

- Pipeline test: pre-create a non-empty dest dir not in state → install fails
  with the "not created by paq" error; same run with `Force: true` succeeds.
- Pipeline test: dest recorded in state from a previous `Run` → reinstall with
  `--force`-less second `Run` of a *different version* succeeds (upgrade
  path unaffected). Note the FEATURE-1 short-circuit skips same-version
  reinstalls before this check, so use two versions.
- Uninstall test: state record with `Dest` = the (test) home dir → uninstall
  fails with "refusing to remove".

**Acceptance.** A typo'd dest can no longer silently destroy an existing
directory; documented upgrade/reinstall flows unchanged; suite green.

---

## M10. Paginate GitHub release asset lookup

**Problem.** `internal/backend/github.go` reads the `assets` array embedded
in `GET /repos/{repo}/releases/tags/{tag}`, which contains at most the first
page (100 entries). Adoptium temurin releases can exceed that; the failure is
a misleading `asset %q not found in release`.

**Changes.**

1. Extend the release response struct with the release id:

   ```go
   type githubRelease struct {
       ID     int64         `json:"id"`
       Assets []githubAsset `json:"assets"`
   }
   ```

2. In `Resolve`, after scanning `release.Assets` without a match **and** when
   `len(release.Assets) == 100` (a full first page — the only case where more
   pages may exist), fetch further pages:

   `GET /repos/{repo}/releases/{id}/assets?per_page=100&page=N` for
   N = 2, 3, ... — same headers/token as the main request. Stop at the first
   page returning fewer than 100 assets or on a match. Factor the "scan slice
   for name" loop into a tiny helper used by both paths.

3. Keep the final error identical.

**Tests.** `internal/backend/github_test.go` uses an `httptest` server:
serve a release with exactly 100 dummy assets and the wanted asset on page 2
of the `/assets` endpoint; assert `Resolve` finds it and that the paged
endpoint was called with the token header. Second case: 100 assets, empty
page 2 → "not found" error.

**Acceptance.** Assets beyond the first 100 resolve; releases with < 100
assets make exactly one HTTP call as today; suite green.

---

## L1. Propagate hashing errors into the state record

**Problem.** `internal/install/pipeline.go`: `filesha256` ignores the
`io.Copy` error, and `Run` ignores `filesha256`'s error
(`artifactSHA256, _ := ...`). A truncated read records a wrong hash.

**Changes.** In `filesha256`, check `io.Copy`'s error and return it. In `Run`,
handle the error: the hash is bookkeeping, not a gate — do not fail the
install; instead warn and store empty:

```go
artifactSHA256, err := filesha256(artifactPath)
if err != nil {
    warn(fmt.Sprintf("could not hash artifact for the state record: %v", err))
    artifactSHA256 = ""
}
```

**Tests.** Unit-test `filesha256` with a nonexistent path (error) and a known
file (correct digest). The warn path is not worth a dedicated test.

**Acceptance.** State never records a hash produced by a failed read.

---

## L2. Extract-by-basename: skip directories, reject ambiguity — DONE

**Problem.** Both `internal/archive/tar.go` and `zip.go` in Extract mode match
*any* entry whose basename equals `opts.Extract`; the last match wins
silently. In zip, a *directory* entry whose basename matches produces an empty
file and sets `found = true`.

**Changes.**

1. `zip.go`, Extract branch: add `if f.FileInfo().IsDir() { continue }` (or
   `break` out of the case) before writing. tar already excludes symlinks;
   also skip `tar.TypeDir` explicitly there (currently a directory named `rg`
   would be written as a file via `writeFile` with the dir's reader — empty
   file, same bug).
2. Ambiguity: after setting `found = true`, a second match must fail:

   ```go
   if found {
       return fmt.Errorf("multiple files named %q in archive: ambiguous extract", opts.Extract)
   }
   ```

   in both tar and zip Extract branches, before writing the second match.

**Tests.** `archive_test.go`: tar with `bin/rg` and `debug/rg` → error
mentioning "ambiguous"; zip with a directory `rg/` and file `sub/rg` →
extracts the file, not an empty one.

**Acceptance.** Extract mode either installs exactly the one intended file or
fails loudly; suite green. Check the embedded specs still install (all use a
unique basename).

---

## L3. Single-pass multi-binary extraction; dedupe chmod parsing — DONE

**Problem.** `internal/install/binaries.go` calls `archive.Extract` once per
binary — N full decompressions of the same archive. Also `file.go` duplicates
the octal-chmod parsing that `parseFileMode` already implements.

**Changes.**

1. Add multi-target support to the archive package instead of a new mode:
   change `ExtractOpts.Extract string` usage sites? **No** — keep the public
   surface: add a new field `ExtractSet []string` is over-design. Chosen
   approach: add one new option, minimal:

   ```go
   // Extracts: if non-empty, extracts only the files whose basename is in
   // the set (keys) into Dest, once each. Mutually exclusive with Extract
   // and Subdir. Missing names cause an error listing them.
   Extracts []string
   ```

   Implement in `extractTar` and `extractZip` alongside the existing
   single-file branch (track a `found map[string]bool`; after the loop, error
   with the sorted list of missing names). The single-name `Extract` field
   delegates to `Extracts` internally (`Extracts = []string{Extract}`) so the
   matching/dir-skip/ambiguity logic from L2 lives in exactly one place.

2. `InstallBinaries`: build `Extracts` from all `bins[i].From`, call
   `archive.Extract` once, then chmod+rename each file as today. Duplicate
   `From` values across bins are allowed (dedupe the set), since two `To`
   names may point at the same source.

3. `file.go` (`InstallFile`): replace the inline `strconv.ParseUint` block
   with `parseFileMode(chmod)` + `if mode != 0 { os.Chmod(...) }`.

**Tests.** `binaries_test.go`: archive with 3 binaries installed via one
`InstallBinaries` call → all present with right modes; a `From` that does not
exist in the archive → error naming it. Existing tests keep passing.

**Dependency.** Land after L2 (shares the Extract matching code).

**Acceptance.** One decompression per install regardless of the number of
binaries; behavior otherwise identical; suite green.

---

## L4. Non-interactive uninstall requires `--yes`

**Problem.** `cmd/paq/uninstall.go`: when stdout is not a TTY, the
confirmation is *skipped* and removal proceeds. Scripts destroy files with no
opt-in.

**Changes.** Invert the non-TTY behavior:

```go
if !flagUninstallYes {
    if !ui.IsTTY() {
        return fmt.Errorf("refusing to uninstall without confirmation in a non-interactive session: pass --yes")
    }
    printUninstallTargets("This will remove:", targets)
    if !confirmYesNo(os.Stdin, "Continue?") { ... }
}
```

Update the flag help text ("required in non-interactive sessions"). Update any
e2e script (`.sdlc/e2e`) that relies on the old behavior to pass `--yes`.

**Tests.** `uninstall_test.go`: stub `ui.IsTTY` (add a package var seam if one
does not exist, mirroring `stderrIsTTY` in `update_notify.go`) → non-TTY
without `--yes` fails and removes nothing; with `--yes` proceeds.

**Acceptance.** No file is ever removed without either a TTY confirmation or
an explicit `--yes`. This is a breaking behavior change: call it out in the
release notes.

---

## L5. Cross-process lock for the state file

**Problem.** `internal/state`'s mutex serializes goroutines, not processes.
Two concurrent `paq` invocations lose each other's `state.json` updates
(load-modify-save race).

**Changes.** Lock-file approach, no new dependency:

1. In `state.Update` only (readers stay lock-free; last-write-wins within a
   single process is already handled by `mu`), acquire
   `<state.json>.lock` before Load and release after Save:

   - `os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)`; on
     `os.IsExist`, retry every 100ms up to 5s, then fail with
     `"state is locked by another paq process (remove %s if stale)"`.
   - Write the PID into the lock file for diagnostics; remove it in a defer.

   O_EXCL-create is portable to Windows (flock is not) and paq's writes are
   millisecond-scale, so bounded retry is acceptable. Document the staleness
   escape hatch in the error itself.

2. Extract `lockedUpdate` into `state.go` next to `Update`; `mu` stays (it
   avoids self-contention between goroutines of the same process burning the
   retry budget).

**Tests.** In `state_test.go`: create the lock file manually → `Update` fails
after the timeout with the message (shrink the timeout via an unexported var,
following the `registryMaxBytes` pattern); happy path: `Update` leaves no lock
file behind.

**Acceptance.** Two sequentially-overlapping `Update` calls from different
processes cannot lose a record; readers unaffected.

---

## L6. Sign SHA256SUMS and verify it in self-update

**Problem.** `paq self-update` verifies the binary only against a same-origin
`SHA256SUMS`: transport integrity, not authenticity. The minisign
infrastructure already exists for the registry.

**Changes.**

1. `.github/workflows/release.yml`, "Sign registry checksums" step: also run
   `minisign -S -s /tmp/minisign.key -m artifacts/SHA256SUMS` and add
   `artifacts/SHA256SUMS.minisig` to the release files list.
2. `cmd/paq/self_update.go`: after downloading `SHA256SUMS`, if
   `registry.DefaultPublicKey != ""`, download `SHA256SUMS.minisig` (asset
   name constant `selfUpdateChecksumsSig = "SHA256SUMS.minisig"`) and pass
   `MinisignPubKey`/`MinisignSigPath` in the existing `verify.Plan` (its Run
   already verifies signature-then-checksum in the right order).
   **Compatibility rule:** if the sig asset is missing from the release
   (releases published before this change), fail — do not fall back —
   *when the running build has a public key*; builds without a key keep
   today's checksum-only behavior. A downgrade-to-unsigned fallback would
   nullify the feature.
3. Reuse the same key pair as the registry (one trust anchor). No new config.

**Tests.** `self_update_test.go` already stubs the release server: add a
signed fixture (generate a throwaway minisign key pair in-test via the
go-minisign API if it supports signing; otherwise commit tiny fixture files
under `testdata/`), assert: valid sig → update proceeds; tampered SHA256SUMS →
fails; key present but sig asset 404 → fails.

**Dependency.** Requires H2 (key injection) to be meaningful in releases.

**Acceptance.** A release-build self-update refuses unsigned or tampered
checksum files; dev builds (no key) behave as today.

---

## L7. Housekeeping batch — DONE

Small, zero-risk cleanups; land as one commit.

1. `internal/install/pipeline.go`: `step(fmt.Sprintf("Downloading checksum
   file..."))` → `step("Downloading checksum file...")`.
2. `internal/install/pipeline.go`, asset-name resolution: stop swallowing the
   template error:

   ```go
   if spec.Asset != "" {
       name, err2 := template.Resolve(spec.Asset, vars)
       if err2 != nil {
           return fmt.Errorf("resolve asset name: %w", err2)
       }
       assetName = name
       vars.Extra["asset"] = name
   }
   ```

   (For the github backend the same template already failed earlier in
   `gb.Resolve`, so this only surfaces errors previously hidden on the url
   backend.)
3. `internal/install/pipeline.go`: move the `extract`/`binaries`
   mutual-exclusion check *after* `spec = spec.ApplyOSOverride(plat.OS)` so a
   per-OS override cannot bypass it. The minisign validation added for H1
   stays where it is (verify config is not OS-overridable).
4. `embedded/registry/temurin.toml`: translate the header comment to English
   (CLAUDE.md: code comments in English). Same for the Italian comments in
   `.sdlc/build` / `.sdlc/cross` / `release.yml` if touched by other items —
   do not do a repo-wide sweep beyond files already being modified.
5. `internal/download/download.go`: reject non-http(s) schemes in `toTemp`
   (`req.URL.Scheme` must be `http` or `https`) — cheap hardening, keeps
   `file://` and friends out of the pipeline.

**Acceptance.** `go vet` clean, suite green, no behavior change except the
now-surfaced asset template error and the scheme rejection.

---

## Suggested order

| Order | Item | Reason |
|-------|------|--------|
| 1 | L7 | trivial, unblocks clean diffs elsewhere |
| 2 | M5, M6 | verification correctness, small and local |
| 3 | M1, M2, M3, M4 | config/template semantics, independent of each other |
| 4 | M8, L2, L3 | archive layer (L3 depends on L2) |
| 5 | M7, M10 | network hardening |
| 6 | M9, L4, L5 | destructive-operation safety |
| 7 | L6 | depends on H2 being released |
