# paq — test suite quality audit

Snapshot of `claude/test-suite-quality-analysis-q715lu` at `e31b6ed`, version
`0.0.16-SNAPSHOT`.

This is a *quality* audit of the tests themselves, not a feature/coverage audit
(that is `AUDIT.md`). It answers seven questions: which tests assert nothing
useful, where the suite gives false safety, what is redundant, which edge cases
are missing, how the suite scores against objective criteria, which code
mutations survive it, and whether the overall test strategy is balanced. §8,
added afterwards, covers a dimension the seven do not: the suite runs on one of
the three operating systems `paq` ships for.

Every claim below was verified by running something. Where a finding is an
experiment, the command is given so it can be re-run.

---

## 0. Snapshot and method

| Measure | Value |
|---|---|
| Test files | 61 (3 of them platform-constrained — see §8) |
| Lines of test code | 10,551 (vs 9,090 lines of production code — ratio 1.16:1) |
| Top-level `Test*` functions | 331 — 330 built on linux, 328 on darwin, 323 on windows (§8) |
| `t.Run` sub-test call sites | 41 |
| `Benchmark*` / `Fuzz*` functions | 0 / 0 |
| `t.Parallel()` call sites | 0 |
| Wall time, `go test ./...` | 2.7 s |
| Wall time, `./.sdlc/test` (`-shuffle=on -race`) | ~4 s |
| Statement coverage, per-package profiles | 62.1 % – 100 % (`internal/ui` 2.1 %) |
| Statement coverage, `-coverpkg=./...` | **74.0 %** |
| Functions at 0 % under `-coverpkg=./...` | 19 |
| Hand-built mutants applied | 26 (9 killed, **17 survived**) |

Two measurement notes that matter for reading the rest of this document:

1. **The per-package coverage numbers under-report reality.** `internal/ui`
   reads as 2.1 % because its own package test only covers `colWidths`; under
   `-coverpkg=./...` its table and log renderers sit at 70–100 %, exercised
   indirectly by the `cmd/paq` tests. The opposite caveat applies too: that
   coverage is *incidental execution*, not verification — see §2.3.
2. **Coverage is a poor proxy here.** The pipeline (`internal/install`) is at
   83 % of functions and 77.8 % of statements, yet deleting its entire
   signature-verification step leaves the suite green (§6, M13). The mutation
   experiment is the more honest instrument.

Reproduce:

```sh
go test ./... -coverpkg=./... -coverprofile=cover.out && go tool cover -func=cover.out | tail -1
```

---

## 1. Tests that do not test anything

Ordered by how much false confidence each one buys.

### 1.1 `TestInstallRipgrep` — asserts an exit code, then logs the answer

`e2e/e2e_test.go:39`

```go
out, err := exec.Command(dest, "--version").Output()
if err != nil { t.Fatalf("rg --version failed: %v", err) }
t.Logf("rg --version: %s", out)
```

**Why it is weak.** This is the only end-to-end test in the repository, and its
last act is to throw away the one piece of evidence it collected. It asserts
"the binary exists and exits 0", nothing about *which* binary.

**Realistic pass-with-broken-code scenario.** A recipe or version-resolution
bug installs ripgrep 13.0.0 when `latest` is 14.1.1 — or the archive contains a
shim, or `Extract: "rg"` picks the wrong entry of two. `rg --version` exits 0
in all of those. The test is green; users get the wrong tool. The same hole
covers the state record: nothing checks that `paq` recorded the version it
actually installed.

**Improved version.**

```go
st, err := state.Load()
if err != nil { t.Fatal(err) }
recs := st.ByName("rg")
if len(recs) != 1 { t.Fatalf("state has %d records for rg, want 1", len(recs)) }

out, err := exec.Command(dest, "--version").Output()
if err != nil { t.Fatalf("rg --version failed: %v", err) }
// "ripgrep 14.1.1" — the binary must report the version paq recorded.
if !strings.Contains(string(out), recs[0].Version) {
    t.Errorf("rg --version = %q, want it to report the installed version %s", out, recs[0].Version)
}
if !strings.HasPrefix(string(out), "ripgrep ") {
    t.Errorf("rg --version = %q, want a ripgrep banner", out)
}
```

### 1.2 `TestOfflineDegradation` — "it does not return an error"

`cmd/paq/registry_offline_test.go:29`

```go
cmds := map[string]func() error{
    "registry list":   func() error { return runRegistryList(nil, nil) },
    "registry status": func() error { return runRegistryStatus(nil, nil) },
    "doctor":          func() error { return runDoctor(nil, nil) },
}
for name, run := range cmds {
    if err := run(); err != nil { t.Errorf("%s failed on corrupt cache: %v", name, err) }
}
```

**Why it is weak.** The test's stated purpose is "read-only commands fall back
to the embedded registry". It verifies the fallback happened only for
`loadConfig` (line 40); for the three commands it verifies only that nothing
blew up. Their *output* — the thing the user sees, and the thing a broken
fallback would empty out — is never captured.

**Realistic pass-with-broken-code scenario.** `runRegistryList` starts printing
an empty table when the external snapshot is unusable (the embedded specs are
silently dropped somewhere in the overlay path). `paq registry list` shows zero
tools, `paq doctor` shows a registry row with 0 specs, and both still return
`nil`. Test green.

**Improved version.** Capture stdout and assert the embedded registry is
actually visible, plus assert the warning is emitted:

```go
out := captureStdout(t, func() {
    if err := runRegistryList(nil, nil); err != nil { t.Fatalf("registry list: %v", err) }
})
if !strings.Contains(out, "ripgrep") {
    t.Errorf("registry list fell back to an empty registry:\n%s", out)
}
```

### 1.3 `TestRegistryUpdateRequiresPublicKey` — passes for the wrong reason

`cmd/paq/registry_update_test.go:244`

```go
setupEnv(t, "https://example.com/registry.tar.gz", "")
if err := runUpdate(t, false); err == nil {
    t.Fatal("update should require public_key for a custom url")
}
```

**Why it is weak.** The only assertion is `err != nil`, against a URL that
points at the real internet. Every failure mode of that call — DNS, proxy, TLS,
404, timeout — satisfies it.

**Proven.** Deleting the public-key check in `resolveRegistrySource`
(`cmd/paq/registry_update.go:197`) leaves the whole suite green (§6, M26). The
test that exists to pin a *security* precondition does not detect its removal.

**Improved version.** Assert the reason, and never leave the reason reachable
only through the network:

```go
err := runUpdate(t, false)
if err == nil || !strings.Contains(err.Error(), "requires public_key") {
    t.Fatalf("error = %v, want it to name the missing public_key", err)
}
```

The sibling `TestRegistryUpdateRejectsHTTP` (line 237) has the same shape and
needs the same fix (`must use https://`).

### 1.4 `TestRunUpgradeMultiArgFailsFastOnUnknownName` — name promises more than the body checks

`cmd/paq/upgrade_test.go:18`

```go
if err := runUpgrade(upgradeCmd, []string{"rg", "typo-xyz-does-not-exist"}); err == nil {
    t.Error("expected an error when the second argument is unknown")
}
```

**Why it is weak.** "Fails fast" means *nothing is attempted*. The test only
checks that an error comes back. The install counterpart
(`cmd/paq/install_test.go:171`) does it properly, asserting the manifest was not
written.

**Proven.** Deleting the entire pre-validation loop in `runUpgrade`
(`cmd/paq/upgrade.go:55-59`) leaves the suite green (§6, M25): the batch then
runs, `rg` gets upgraded, the unknown name fails inside `runParallel`, and an
error still surfaces.

**Improved version.** Seed state for `rg`, then assert the state is untouched:

```go
before, _ := state.Load()
err := runUpgrade(upgradeCmd, []string{"rg", "typo-xyz"})
if err == nil { t.Fatal("expected an error") }
after, _ := state.Load()
if !reflect.DeepEqual(before.Packages, after.Packages) {
    t.Error("an invalid later argument must prevent every upgrade, state changed")
}
```

### 1.5 `TestRunSelfUpdateForceProceedsWhenUpToDate` — the assertion is "something broke"

`cmd/paq/self_update_test.go:187`

```go
err := runSelfUpdate(selfUpdateCmdWithFlags(t, false, true), nil)
if err == nil { t.Fatal("expected an error once --force proceeds to a real (unfixtured) download, got nil") }
if !otherRequest.Load() { t.Error("--force did not attempt to resolve/download release assets") }
```

**Why it is weak.** The `otherRequest` assertion is the real one and it is
sound. But the first assertion encodes "the fixture is incomplete" as the
expected behaviour: if `--force` later grows a legitimate early return that
still touches an endpoint, or if the error becomes a different one entirely, the
test cannot tell. The comment admits it ("the failure itself is the proof").

**Improved version.** Drop the error assertion or make it specific
(`want a 404 for the missing asset`), and keep `otherRequest` as the contract.
Better: give the fixture the asset endpoints and assert the successful path,
with `replaceExecutable` behind a seam (it is at 0 % coverage today).

### 1.6 `TestRunDoctorSucceedsWhenNothingIsBroken` — health check with no health assertions

`cmd/paq/doctor_test.go:52` asserts `runDoctor(...) == nil` for three manifests
and nothing else. Its `--json` sibling (line 168) does assert `Problems == 0`
and `len(Checks) > 0`, which is what the human-output test should do too. As
written, a `doctor` that silently stops running half its checks passes: fewer
checks means fewer problems means `nil`.

**Improved version.** Assert the row set, so a check that disappears is caught:

```go
out := withJSON(t, func() { runErr = runDoctor(doctorCmd, nil) })
var report doctorReport
// ... unmarshal ...
got := map[string]bool{}
for _, c := range report.Checks { got[c.Name] = true }
for _, want := range []string{"platform", "config", "registry", "state", "bin-dir"} {
    if !got[want] { t.Errorf("doctor report is missing the %q check: %+v", want, report.Checks) }
}
```

### 1.7 Smaller cases of the same shape

| Test | Problem |
|---|---|
| `internal/verify/sha256_test.go:27` | `CheckFile(f, "deadbeef")` — an 8-char hash. A mismatch and a malformed digest are indistinguishable; use a valid-length wrong digest (as `cmd/paq/exitcode_test.go:58` correctly does). |
| `internal/verify/sha512_test.go:26` | Comment says "uppercase/spaces (normalization)" but the fixture is `"  "+expected+"\n"` — lowercase. The uppercase half of the claim is untested; the matching `CheckFile` normalization is a live survivor (§6, M5). |
| `internal/version/latest_test.go:9,30` | Asserts the concrete provider *type* returned by the factory and one field. No behaviour. Acceptable as wiring tests, but they are the only thing standing behind `LatestProvider`. |
| `internal/registry/registry_test.go:39` | `TestOpenAbsent` asserts `(nil, nil, nil)` — correct, but it is the only assertion, so "absent" and "present but empty" are the same to the suite. |
| `cmd/paq/completion_test.go:87` | `TestCompleteRegistrySpecs` asserts that `"ripgrep"` is among the completions, i.e. against the embedded registry's contents rather than against the completion logic (§2.5). Retiring the `ripgrep` recipe would break a completion test. |

---

## 2. False safety and over-mocking

Severity is "how likely is this to hide a real defect", not "how ugly is it".

### 2.1 HIGH — `captureStdout` leaks `os.Stdout` when the captured function fails

`cmd/paq/json_empty_test.go:14`

```go
orig := os.Stdout
os.Stdout = w
fn()          // ← a t.Fatal inside fn() unwinds via runtime.Goexit
w.Close()
os.Stdout = orig
```

Almost every call site is `captureStdout(t, func() { if err := ...; t.Fatalf(...) })`.
A `t.Fatalf` inside `fn` calls `runtime.Goexit`, so the restore never runs and
`os.Stdout` stays pointing at a **closed pipe** for the rest of the package.

**Proven** with a standalone reproduction of the helper: after a test that
fails inside the capture, the next test observes `os.Stdout.Fd() == 9`, not `1`.
The practical effect is that once one output test fails, every later
`t.Log`/`fmt.Print` in that package is silently swallowed — exactly when the
diagnostics matter most.

The same helper has a second, latent failure: nothing drains the pipe until
`fn` returns, so any capture above the ~64 KiB pipe buffer **deadlocks**. A
200 KiB write hangs until the test binary's timeout panic (verified). No test
prints that much today; `ls`/`registry list` with a large state would.

**Refactor.**

```go
func captureStdout(t *testing.T, fn func()) string {
    t.Helper()
    r, w, err := os.Pipe()
    if err != nil { t.Fatalf("create pipe: %v", err) }
    orig := os.Stdout
    os.Stdout = w
    // Restore even if fn calls t.Fatal (runtime.Goexit).
    defer func() { os.Stdout = orig }()

    // Drain concurrently so a capture larger than the pipe buffer cannot deadlock.
    done := make(chan []byte, 1)
    go func() { b, _ := io.ReadAll(r); done <- b }()

    fn()
    w.Close()
    return string(<-done)
}
```

`withJSON` (line 38) already uses `defer` for `ui.Global` and is fine.

### 2.2 HIGH — `internal/install` monkey-patches `http.DefaultTransport`

`internal/install/pipeline_test.go:668`, `:749`, `:891`

```go
origTransport := http.DefaultTransport
http.DefaultTransport = &redirectTransport{base: srv.URL, inner: origTransport}
defer func() { http.DefaultTransport = origTransport }()
```

This is the one place in the suite that mutates process-global HTTP state. Three
tests do it, and the pipeline's own comment block (lines 663-667) admits it is a
workaround: `install.Run` builds its client with `download.NewClient()` and
offers no injection point, while `backend.GitHubBackend` and every
`version.*Provider` *do* accept an `HTTPClient`.

**Why it is a false-safety risk.** The transport swap is not the thing under
test, but it is what the tests depend on, and it makes the GitHub-backend half
of the pipeline untestable in any other way. It is also the one construct that
would break the moment anyone adds `t.Parallel()` to the package.

**Refactor.** Add a client seam to the pipeline, mirroring what the lower
layers already have:

```go
// Hooks (or a new Options struct)
type Hooks struct {
    // ...
    // HTTPClient overrides the default download client (tests, proxies).
    HTTPClient *http.Client
}
```

then `client := hooks.HTTPClient; if client == nil { client = download.NewClient() }`
and pass it into `backend.GitHubBackend{HTTPClient: client}`. The three tests
then need no globals, and the seam is the same one the rest of the codebase uses.

### 2.3 MEDIUM — the renderers are executed, never verified

`internal/ui` has exactly one real test (`colWidths`, `table_test.go:8`). Under
`-coverpkg=./...` its renderers report 70–100 % coverage because the `cmd/paq`
tests print through them. Every assertion on that output is a
`strings.Contains` on one or two cells:

```go
for _, want := range []string{"Config", "Defaults", "~/.custom/bin", "rg", "14.1.1"} {
    if !strings.Contains(out, want) { ... }   // cmd/paq/config_show_test.go:28
}
```

**What this hides.** Column misalignment, a dropped column, a row printed
twice, a truncated value, ANSI codes leaking into a non-TTY pipe, `formatBinaries`
(22 % of statements covered) rendering `[]` for a multi-binary spec. The
`ls` test explicitly asserts the *absence* of columns
(`cmd/paq/ls_test.go:55`) but never the presence of the row shape.

**Refactor.** Table rendering is a pure function of its input; test it in
`internal/ui` with golden output, not through the commands:

```go
func TestPrintLsTableRows(t *testing.T) {
    out := capture(func() { PrintLsTable([]LsEntry{{Name: "bat", Version: "0.24.0", ...}}) })
    want := "NAME  VERSION  KIND  DEST\nbat   0.24.0   file  /x/bat\n"
    if out != want { t.Errorf("table =\n%q\nwant\n%q", out, want) }
}
```

Keep the `cmd/paq` tests asserting *behaviour* (JSON shape, exit codes, which
entries appear) and stop asserting *formatting* there.

### 2.4 MEDIUM — package-level flag globals are the test fixture

`flagInstallForce`, `flagInstallNoSave`, `flagUninstallYes`, `flagUninstallDryRun`,
`flagInitForce`, `flagJSON`, `flagQuiet`, `flagDebug`, `Version`, `ui.Global`,
`uninstallIsTTY`, `stderrIsTTY`, `spawnBackgroundCheck`, `selfUpdateClient`,
`registryUpdateClient`, `registryMaxBytes`, `registry.DefaultPublicKey`,
`config.PathOverride`, `httpretry.BaseDelay`, `httpretry.OnRetry`,
`state.lockRetryInterval`, `state.lockTimeout`.

Most sites restore correctly with `t.Cleanup`, and `-shuffle=on` is in
`.sdlc/test`, which is why this is MEDIUM and not HIGH. The inconsistencies:

- `cmd/paq/ls_test.go:70` sets `ui.Global.JSON = true` and restores
  `ui.Config{}` — the *zero value*, not the saved value. `withJSON`
  (`json_empty_test.go:38`) and `withJSONFlag` (`root_test.go:15`) both save and
  restore properly. Three idioms for one concern.
- `cmd/paq/upgrade_test.go:54` does `upgradeCmd.SetContext(context.Background())`
  then `t.Cleanup(func() { upgradeCmd.SetContext(nil) })` on the **shared global
  command object**. `cmdWithContext()` (`outdated_test.go:21`) and
  `selfUpdateCmdWithFlags` (`self_update_test.go:121`) already show the right
  pattern: build a throwaway `*cobra.Command`.
- `cmd/paq/install_test.go:176` and `uninstall_test.go:61` set flags to `false`
  before the test *and* restore them to `false` after — restoring a hardcoded
  default rather than the previous value.

**Refactor.** One helper per global, saving and restoring the previous value,
used everywhere:

```go
func withFlag[T any](t *testing.T, p *T, v T) { t.Helper(); old := *p; t.Cleanup(func() { *p = old }); *p = v }
```

and never mutate `installCmd`/`upgradeCmd`/`uninstallCmd` — pass a throwaway
command.

### 2.5 MEDIUM — production registry data used as test fixture

`internal/config/load_test.go` asserts against `embedded/registry/*.toml`:

- `TestMicroSpec` (line 154) hardcodes `"micro-2.0.15-linux64.tar.gz"`, derived
  from `micro.toml`'s `default_version = 2.0.15`.
- `TestVSCodeSpec` (line 206) hardcodes update-service URLs and
  `LatestJSON == "productVersion"`.
- `TestRunpSpec` (line 78) hardcodes `runp`'s asset template and platform list.
- `TestLoadEmbeddedRegistry` (line 13) hardcodes ripgrep's repo, jdk's
  `strip_components`, and `zipp.Binaries` length `== 3`.

**Why this is false safety.** These read as tests of the *loader* but they are
tests of the *data*. Bumping a curated `default_version` — a routine registry
maintenance commit that touches no Go code — breaks `TestMicroSpec`. Meanwhile
the loader behaviour they nominally cover (per-OS override, per-OS-arch
override, arch mapping, template expansion) is already covered properly, with
synthetic fixtures, by `internal/config/overlay_test.go` and
`TestUserConfigSpecs` (`load_test.go:327`).

**Refactor.** Split the concerns:

1. Keep *one* smoke test that the embedded registry parses and is non-empty:
   `for name, s := range specs { if s.Backend == "" { t.Errorf("%s: no backend", name) } }`
   — plus a structural validity pass (every spec has a backend, a `github`
   backend has a repo, a `url` backend has a source, `extract`/`binaries` are
   not both set). That catches real registry breakage and never needs updating.
2. Move the behavioural assertions (asset naming per os/arch, windows archive
   override, meta-template expansion) onto synthetic specs in the test file, as
   `TestUserConfigSpecs` already does.

### 2.6 LOW — `TestRegistryUpdateOversize` asserts on the shared temp dir

`cmd/paq/registry_update_test.go:302`

```go
before, _ := filepath.Glob(filepath.Join(os.TempDir(), "paq-download-*"))
// ...
after, _ := filepath.Glob(filepath.Join(os.TempDir(), "paq-download-*"))
if len(after) > len(before) { t.Errorf("leftover paq-download temp files: ...") }
```

`go test ./...` runs packages concurrently, and `internal/download` writes
`paq-download-*` into the same `os.TempDir()`. A download in flight in the other
package during this window makes this fail. `internal/download`'s own leftover
check does it right (`download_test.go:88`): `t.Setenv("TMPDIR", t.TempDir())`,
then the assertion is exact. Copy that.

### 2.7 What is *not* over-mocked — worth preserving

The suite's dominant style is a real `httptest.Server` plus a real filesystem
under `t.TempDir()`, with no mocking framework and no hand-written fakes of the
code under test. That is the right choice for a package manager and it is why
several tests are genuinely strong:

- `TestPipelineSkipsWhenAlreadyInstalled` (`pipeline_test.go:1219`) counts HTTP
  requests with an `atomic.Int32` instead of asserting a log line — the skip is
  proven by the absence of work.
- `TestPipelineUsesLockedVersionInsteadOfLatest` (`lock_test.go:40`) proves the
  lockfile short-circuit the same way: `latestHits == 0`.
- `TestExitCodeForRealVerificationFailures` (`exitcode_test.go:57`) drives real
  `verify.Run` failures through `exitCodeFor` instead of constructing the error,
  explicitly to stop message rewording from downgrading exit code 4.
- `TestDoGivesUpAfterMaxAttempts` (`httpretry_test.go:159`) asserts the literal
  `3`, with a comment explaining that asserting against `maxAttempts` would make
  the test follow the constant. This is exactly right, and it is why M12 died.

These four are the model the rest of the suite should be measured against.

---

## 3. Duplicated and redundant tests

### 3.1 Duplicated infrastructure (4 copies of one transport, 3 of one signer)

| Concern | Copies |
|---|---|
| Rewrite-host `http.RoundTripper` | `internal/backend/github_test.go:217` (`rewriteTransport`), `internal/version/github_release_test.go:139` (`prefixRoundTripper`), `internal/install/pipeline_test.go:1013` (`redirectTransport`), `cmd/paq/self_update_test.go:38` (`selfUpdateRewriteTransport`) — four identical implementations, one with a comment stating it is a copy |
| minisign test keypair + signing | `internal/verify/minisign_test.go:17` (`newTestMinisignKey`), `cmd/paq/registry_update_test.go:35` (`newSigner`) |
| sha256 hex of a buffer | `pipeline_test.go:43` (`sha256hex`), `verify_test.go:14` (`sha256Hex`), `registry_update_test.go:90` (`sha256Line`) |
| sha512 hex of a buffer | `pipeline_test.go:48` (`sha512hex`), `verify_test.go:19` (`sha512Hex`) |
| tar.gz builder | `archive_test.go:18`, `pipeline_test.go:32`, `registry_update_test.go:64` (three different signatures) |
| zip builder | `archive_test.go:66` (`makeZip`), `pipeline_test.go:55` (`makeFakeZip`), `binaries_test.go:15` (`makeMultiBinZip`) |

**Recommendation.** Create `internal/testutil` (or `internal/paqtest`) holding:
`RewriteTransport`, `Signer` (keypair + `Sign`), `SHA256Hex`/`SHA512Hex`,
`TarGz(map[string]string)`, `Zip(map[string]string)`. Six helpers replace
~15 copies. This is not cosmetic: the four transports differ subtly (two keep
`inner`, two hardcode `http.DefaultTransport`), and the `archive` and `install`
tar builders silently ignore `WriteHeader`/`Write` errors, so a malformed
fixture would surface as a confusing extraction failure.

### 3.2 Redundant test cases, grouped by behaviour covered

**Group A — "minimum_release_age is not honoured by this backend, so warn".**

| Test | File |
|---|---|
| `TestResolveLatestVersionWarnsOnUnsupportedBackendWithExplicitAge` (2 sub-cases) | `cmd/paq/upgrade_test.go:141` |
| `TestResolveLatestVersionWarnsForArchLinuxStrategy` | `cmd/paq/upgrade_test.go:182` |
| `TestResolveLatestVersionNoWarnWithoutExplicitAge` | `cmd/paq/upgrade_test.go:205` |
| `TestPipelineMinimumReleaseAgeWarnsOnUnsupportedBackend` | `internal/install/pipeline_test.go:797` |

Five scenarios for one predicate, in two packages. The `cmd/paq` four differ
only in which field carries the value; they are already half-parameterised and
should be one table with a `wantWarnings int` column. The pipeline one duplicates
the same predicate through a second entry point — keep it (the pipeline has its
own copy of the logic at `pipeline.go:191`), but as a single case, not as the
place where the warning's content is re-verified.

**Group B — "invalid minimum_release_age fails fast".**
`TestResolveLatestVersionInvalidMinimumAge` (`upgrade_test.go:224`),
`TestPipelineMinimumReleaseAgeInvalidFailsFast` (`pipeline_test.go:769`),
`TestResolveMinimumAgeInvalid` (`internal/version/age_test.go:69`). Three tests,
same `version.ParseAge` failure, three layers. The `internal/version` one is the
real test; the other two are wiring checks and can be one case each inside the
Group A table.

**Group C — "the warning fires / does not fire when verify is configured".**
`TestPipelineWarnsWhenNoVerify` (`pipeline_test.go:513`) and
`TestPipelineNoWarnWhenVerifyConfigured` (line 558) are 45 lines each and differ
by one struct field. One table-driven test with `wantWarn bool`, two cases.

**Group D — bare-hash checksum parsing.** `TestRunSHA256AssetBareHash`
(`verify_test.go:78`) and `TestRunSHA512AssetBareHash` (line 101) are the same
test at two digest widths, and both re-cover what
`TestParseSHA256FileBareHash` (`sha256_test.go:78`) /
`TestParseSHA512FileBareHash` (`sha512_test.go:39`) already cover at the parser
level. The `Run`-level pair is worth *one* test (the wiring from `Plan` to
parser); the second is redundant.

**Group E — "one-field line does not short-circuit".**
`TestParseSHA256FileOneFieldLineDoesNotShortCircuit` (`sha256_test.go:92`) and
`TestParseSHA512FileOneFieldLineDoesNotShortCircuit` (`sha512_test.go:71`) are
byte-for-byte the same test at two widths; same for the
`MalformedBareHash` and `NameNotFound` pairs (6 tests, 3 behaviours). Either
parameterise over `{parse func, digest string}` or accept the duplication
consciously — these two parsers are separate code paths, so this is the
*defensible* duplication in the list.

**Group F — pipeline happy paths.** `TestPipelineSHA512URLBackend` (line 73),
`TestPipelineOmittedVersionUsesDefault` (line 130),
`TestPipelineOmittedDestUsesDefaults` (line 174),
`TestPipelineWarnsWhenNoVerify` (line 513),
`TestPipelineNoWarnWhenVerifyConfigured` (line 558),
`TestPipelineDebugHook` (line 962) each rebuild the *same* maven fixture: same
`makeFakeZip("apache-maven-1.0.0", "bin/mvn", ...)`, same `httptest` switch over
`.zip`/`.zip.sha512`, same `config.Config` literal — ~35 lines duplicated six
times, ~210 lines total. They do test different things, so none should be
deleted; they should share a fixture builder:

```go
// mavenFixture serves a zip (+ optional sha512) and returns the config and dest.
func mavenFixture(t *testing.T, opts ...fixtureOpt) (*config.Config, string)
```

**Safe to delete outright (no semantic coverage lost):**

- `internal/version/clean_test.go:97` `TestCompareIgnoresBuildMetadata` — the
  case `{"1.2", "1.2.0", 0}` in `TestCompare` plus `TestBuild` already establish
  this; the test is a comment with an assertion attached.
- `internal/config/dest_test.go:38` `TestDefaultDestRootsPartialOverride`
  overlaps `TestDefaultDestUserDefaults` (line 27); merge into one table.
- `cmd/paq/uninstall_test.go:263` `TestParseAppRef` and
  `cmd/paq/import_test.go:41` `TestValidAppKey` are fine, but
  `TestRenderAppEntryTOMLOmitsEmpty` (`import_test.go:34`) is one assertion that
  belongs as a case in `TestRenderAppEntryTOML`.

**Net effect if applied:** roughly 350–450 lines of test code removed, ~12 test
functions merged into 4 tables, no loss of covered behaviour.

---

## 4. Missing edge cases

The reference component is `internal/install.Run` (`pipeline.go:74`) — the
program's core, 480 lines, 20 decision points. Its branch map, with coverage:

| # | Decision | Line | Covered by |
|---|---|---|---|
| 1 | app missing from manifest | 109 | ✗ (only via `cmd/paq`) |
| 2 | `app.Use` empty → spec name = app name | 113 | ✗ **no test** |
| 3 | spec missing from registry | 117 | ✗ **no test** |
| 4 | half-configured minisign | 127 | ✓ `TestPipelineHalfConfiguredMinisignFails` |
| 5 | minisign without sha256 source | 130 | ✓ `TestPipelineMinisignWithoutSHA256AssetFails` |
| 6 | `sha256_asset` + `sha256_url` both set | 136 | ✓ `TestPipelineRejectsIncoherentSHA256Config` |
| 7 | `sha256_json` without a document | 139 | ✓ same |
| 8 | platform unsupported | 148 | ✗ **no test** (M3 survives) |
| 9 | no verification → warn | 155 | ✓ `TestPipelineWarnsWhenNoVerify` |
| 10 | version: default / locked / live / pinned | 170-201 | ✓ all four |
| 11 | `ErrLatestNotImplemented` + `default_version` set | 206 | ✗ **no test** (the friendlier message) |
| 12 | already installed → skip | 214 | ✓ `TestPipelineSkipsWhenAlreadyInstalled` |
| 13 | `extract` + `binaries` both set | 227 | ✗ **no test** (M4 survives) |
| 14 | dest template / `~` expansion failure | 247-254 | partial (`TestExpandHomeFailsWithoutHome` unit-level only) |
| 15 | backend github / url / unknown | 259-268 | ✓ / ✓ / ✗ **unknown backend has no test** |
| 16 | asset template failure | 286 | ✓ `TestPipelineAssetTemplateErrorSurfaces` |
| 17 | aux URL resolution (github vs derived) | 301-307 | ✓ both |
| 18 | minisign signature verified before download | 394 | ✗ **no test** (M13 survives) |
| 19 | install kind: file / binaries / dir | 460-517 | ✓ all three |
| 20 | unowned non-empty dest guard | 509 | ✓ `TestPipelineRefusesToReplaceUnownedDest` |
| 21 | state save failure | 522 | ✓ `TestPipelineDoesNotAnnounceSuccessBeforeStateSaveSucceeds` |
| 22 | lock write failure → warn, not fail | 547 | ✗ **no test** |

Below, each missing case with its concrete risk and a proposed test.

### 4.1 The install pipeline never verifies a signature in any test

**Risk.** `pipeline.go:394-400` is the *only* place a minisign signature is
checked during an install: the plan handed to `verify.Run` has its minisign
fields deliberately blanked (lines 429-430). No test configures a valid
`[verify.minisign]` spec end-to-end, so the step is dead weight as far as the
suite is concerned — deleting it keeps every test green (§6, M13). Signature
verification is the headline security feature in `README.md`.

Confirmed twice, independently: line-level coverage for the block
`pipeline.go:394.81-400` is **0** under `-coverpkg=./...` (the whole `if` body
is never entered), and the mutation survives. The same check lists every other
uncovered guard in the table above: `109`, `114.20`, `118.12`, `148.48`,
`206.83`, `227.50`, `266.10`, `547.112`.

**Proposed tests** (`internal/install/pipeline_test.go`):

```go
// TestPipelineVerifiesMinisignSignatureOfChecksum: a correctly signed checksum
// file installs; the signature is checked BEFORE the artifact is downloaded.
func TestPipelineVerifiesMinisignSignatureOfChecksum(t *testing.T)

// TestPipelineRejectsBadMinisignSignature: a checksum file signed by another
// key fails the install, the error mentions the signature, and the artifact
// endpoint is never hit (count requests) — proving order, not just outcome.
func TestPipelineRejectsBadMinisignSignature(t *testing.T)

// TestPipelineRejectsTamperedChecksumWithValidSignature: signature valid for
// the original checksum, checksum body altered → fails at the signature step.
func TestPipelineRejectsTamperedChecksumWithValidSignature(t *testing.T)
```

The fixture already exists in two places (`newSigner`, `newTestMinisignKey`) —
this is the strongest argument for the shared `internal/testutil` of §3.1.

### 4.2 Unsupported platform is never rejected in a test

**Risk.** `pipeline.go:148` is the pre-flight that stops paq from downloading a
`linux/amd64` tarball on `darwin/arm64`. With the check gone, the install
proceeds to a 404 (or worse, to an asset whose name happens to resolve) and the
user gets "download artifact: HTTP 404" instead of "not available for
darwin/arm64 (supported: linux/amd64)". `config.Spec.SupportsPlatform` is
well-tested in isolation (`types_test.go:5`); its *use* is not.

**Proposed test.**

```go
// TestPipelineRejectsUnsupportedPlatformWithoutNetwork verifies the pre-flight
// check: a spec that does not list the running platform fails before any
// request, and the error names the supported list.
func TestPipelineRejectsUnsupportedPlatformWithoutNetwork(t *testing.T) {
    // platforms = ["plan9/mips"] can never match the test runner.
    // Assert: err mentions "is not available for", and the httptest server
    // recorded zero requests.
}
```

### 4.3 `extract` + `binaries` mutual exclusion

**Risk.** `pipeline.go:227`. Without it, `len(spec.Binaries) > 0` loses to
`spec.Extract != ""` in the switch at line 460, so a spec setting both silently
installs one file and ignores the binaries list — a recipe bug that reports
success. This is a registry-authoring guard, so it fires on data paq does not
control.

**Proposed test.** `TestPipelineRejectsExtractWithBinaries`, asserting the error
mentions "mutually exclusive", no network touched.

### 4.4 Unknown backend

**Risk.** `pipeline.go:267` — `default: err = fmt.Errorf("unknown backend: %q")`.
A typo in a user `[specs.x] backend = "gihub"` must produce that message. Today
nothing covers it, and `cmd/paq/errors_test.go` has no `hintFor` case for it
either, so the user gets the generic `--debug` hint.

**Proposed test.** `TestPipelineUnknownBackendErrors` + a `hintFor` case
suggesting the valid values.

### 4.5 Spec/app resolution failures

**Risk.** `pipeline.go:113-120`. Case 2 (`app.Use == ""` falls back to the app
name) is a real feature — `[apps.ripgrep]` with no `use` — and is used by the
auto-import path (`ensureManifestEntry` always sets `Use`, but a hand-written
manifest need not). If the fallback broke, every hand-written manifest without
`use` would fail with `spec "" not found in registry`.

**Proposed tests.** `TestPipelineUsesAppNameWhenUseOmitted` (install succeeds
with `AppEntry{Version: "1.0.0", Dest: dest}` and no `Use`), and
`TestPipelineUnknownSpecNamesIt`.

### 4.6 `latest` with a `default_version` available — the guidance message

**Risk.** `pipeline.go:206-208` produces the one error message that *teaches*
the user what to do ("omit the version to use the default 1.2.3, or pin an
explicit version"). `TestPipelineLatestNoStrategyErrors` (line 221) covers the
same branch with `DefaultVersion` empty, i.e. the other side of the `if`.

**Proposed test.** `TestPipelineLatestWithDefaultVersionSuggestsOmittingIt`,
asserting the message contains the default version.

### 4.7 Lockfile write failure must warn, not fail

**Risk.** `pipeline.go:546-552`. The install has already succeeded and the state
is recorded; a read-only config directory must not turn that into a failed
install. Conversely, if someone changes `warn` to `return`, a user with a
root-owned `~/.config/paq` can no longer install anything.

**Proposed test.** `TestPipelineLockWriteFailureOnlyWarns`: point
`config.PathOverride` at a path inside a directory made read-only (or at a
directory-as-file, the trick `TestPipelineDoesNotAnnounceSuccessBeforeStateSaveSucceeds`
already uses), assert `Run` returns `nil`, the state record exists, and exactly
one warning mentions `paq.lock.toml`.

### 4.8 Upgrade cleanup can delete what it just installed

**Risk.** `cmd/paq/upgrade.go:206` `cleanupOldVersions` is at **0 % coverage**,
including its keep-set (`survivingPaths`, line 187). M20 proves the keep-set can
be replaced with an empty map and the suite stays green. The scenario the keep
set exists for is real and documented in the code comment: two versions sharing
a version-independent `dest`, where the pipeline overwrote in place — removing
the "old" record's files deletes the new install.

**Proposed test.**

```go
// TestCleanupOldVersionsKeepsPathsOwnedByTheNewInstall: state holds tool@1.0.0
// and tool@2.0.0 both with Dest = <dir> (the shared, version-independent
// destination). Cleaning up 1.0.0 must leave <dir> on disk and remove only the
// 1.0.0 record.
func TestCleanupOldVersionsKeepsPathsOwnedByTheNewInstall(t *testing.T)

// TestCleanupOldVersionsRemovesVersionSpecificDest: the mirror case —
// /opt/tool-1.0.0 and /opt/tool-2.0.0, cleanup must delete the former.
```

### 4.9 Other packages: the highest-value gaps

| Area | Missing case | Risk |
|---|---|---|
| `internal/install/file.go` | **No test file at all.** `InstallFile` with `extractName == ""` computes `extracted := filepath.Join(tmpDir, "")` = `tmpDir` and renames a *directory* onto `dest`. The doc comment claims this is a supported mode. | A spec with `archive = ""` and no `extract` silently installs a directory where a file is expected |
| `internal/install/file.go:366` | `parseFileMode` with a malformed `chmod` ("u+x", "999", "0o755") | A recipe typo surfaces as a confusing install failure; no test pins the message |
| `internal/archive` | `writeFile` mode handling: M7 (always 0755) and M8 (drop final `Chmod`) both survive | Extracted files get wrong permissions; a non-executable binary installs "successfully" |
| `internal/archive` | Zero-byte archive, truncated gzip mid-entry, tar entry with `Size` larger than the remaining stream, zip with duplicate names, 0-entry zip | Corrupt/truncated download misreported; `Extract` returning nil on an empty archive means `InstallDir` swaps in an empty tree |
| `internal/archive` | `stripComponents(path, n)` where the path has exactly `n` components (`tar.go:206`) — covered only indirectly | An archive whose top dir is the only component silently extracts nothing |
| `internal/download` | Context cancellation mid-download (Ctrl-C), a body that stops early (`Content-Length` > bytes delivered), a redirect chain, a `file://` redirect target | `install` reports success on a truncated artifact if no checksum is configured; there is no test that `ctx` cancellation is honoured anywhere in the codebase |
| `internal/state` | Corrupt `state.json` (not JSON / schema 1 / `packages: null`); `Load` reports the parse error — untested. Schema **migration** from 1 to 2 is untested and `schemaVersion = 2` implies one exists | An upgrade from an older paq either fails hard or silently loses every record |
| `internal/state` | Lock file containing garbage / negative PID / empty (`removeStaleLock`, `state.go:311`) | A corrupted lock file either blocks paq forever or is reclaimed while a live process holds it |
| `internal/verify` | Uppercase digest in a checksum *file* (M5 survives for `CheckFile`); digest with a `sha256:` prefix in a coreutils-style file; CRLF line endings in a checksum file | A publisher using uppercase hashes or Windows line endings fails verification with "tampered" — pointing at the wrong problem |
| `internal/version` | An **old prerelease** (M17 survives — the only prerelease fixture is also too-new); a release with `published_at` exactly at the cutoff (M19 survives); `published_at` missing/zero; a repo whose `/releases` returns 403 with a rate-limit body | `minimum_release_age` installs a prerelease; the "GITHUB_TOKEN" hint never gets tested against a real 403 |
| `internal/config` | Manifest with duplicate `[apps.x]`; `dest` containing `..`; a spec named with characters invalid as a TOML bare key; a 0-byte `config.toml` | A malformed manifest either crashes or is silently half-loaded |
| `cmd/paq` | `runParallel` with `maxParallel` exceeded (>3 apps) — concurrency limit untested; context cancellation of a batch; `installParallel` and `appHooks` at 0 % | The parallel install path — the one place with real concurrency — is covered by a single test with 3 synthetic actions |
| `cmd/paq` | `--json` on every command that allows it, against a *populated* fixture (today several only assert the empty-array case) | A JSON schema regression in a populated response is invisible |

---

## 5. Quality against objective criteria

| Criterion | Score | Justification |
|---|---|---|
| **Isolation** | **3 / 5** | The norm is excellent: `t.TempDir()` + `t.Setenv` + `httptest`, no network, no shared DB, and every `cmd/paq` test that loads the manifest routes it through `doctorEnv`. Three deductions: `captureStdout` leaks `os.Stdout` across tests when the captured function fails (§2.1); `http.DefaultTransport` is monkey-patched by three pipeline tests (§2.2); one test asserts on the shared `os.TempDir()` (§2.6). |
| **Determinism** | **4 / 5** | No `time.Sleep` anywhere in the suite; retry backoff is shrunk via `httpretry.BaseDelay`; `exitedPID` gets a guaranteed-dead PID by running the test binary with `-test.run=^$` instead of guessing; `TestExpandDeterministicCrossReferenceFails` loops 50× specifically to defeat Go's map-iteration randomisation. Deductions: `TestGitHubReleaseProvider*` build fixtures from `time.Now()` with ±1 h margins (fine, but a `clock` seam would be better than margins), `TestMaybeNotifyUpdateSpawnsWhenStale` compares against a wall-clock interval, and the `os.TempDir()` glob is order-dependent across packages. |
| **Readability** | **4 / 5** | The standout strength. Most non-obvious tests carry a comment explaining *which regression* they guard, several citing `AUDIT-OBSERVABILITY.md` items; assertion messages consistently follow `got = X, want Y` with the reason. Deductions: 11 residual Italian comments in an English-only codebase (`install/pipeline_test.go:156,534`, `cmd/paq/errors_test.go:13`, `state/state_test.go:16,289`, `version/github_release_test.go:23,138,145`, `verify/minisign_test.go:82`, `config/write_test.go:92`, `archive/archive_test.go:151`); dead code at `github_release_test.go:24-25` (`origTransport := ...; _ = origTransport`); the 6× duplicated maven fixture (§3.2 Group F) makes those tests longer than their intent. |
| **Order independence** | **4 / 5** | `.sdlc/test` runs `-shuffle=on -race` by default — a deliberate, correct choice, and 3 consecutive shuffled runs pass. Held back from 5 by the mutable-global surface (§2.4) and three restore idioms, two of which restore a hardcoded default rather than the saved value; the `os.Stdout` leak is a genuine cross-test coupling that shuffling cannot catch because it only triggers on failure. |
| **Execution time** | **5 / 5** | 2.7 s plain, ~4 s with `-race -shuffle=on`, no package above 1.6 s. Fast enough that the script can afford `-race` unconditionally and comment that "both are free". No sleeps, no real network, no container fixtures. Nothing to improve. |
| **Signal-to-noise** | **3 / 5** | High signal where it counts: request-counting assertions, error-identity assertions instead of message matching, explicit "must not happen" checks. Noise: ~17 of 26 hand-built mutants survive (§6), so a large share of the suite's 330 tests does not discriminate; ~15 duplicated helpers and ~210 duplicated fixture lines; production registry data as fixtures means routine data commits produce red builds unrelated to the change (§2.5); `strings.Contains`-on-rendered-output as the standard assertion for command behaviour means a test can pass while the output is unusable. |

**Overall: 3.8 / 5.** This is a well-above-average Go test suite — fast,
deterministic, honest about integration, unusually well-commented — whose main
weakness is that its *assertions* are weaker than its *setup*. The suite
executes almost everything (74 % statements) and verifies noticeably less.

### The five priority actions

1. **Close the signature-verification hole** (§4.1). Three tests, one shared
   fixture. It is the single highest-severity gap: the security feature the
   README leads with can be deleted without turning the build red.
2. **Fix `captureStdout`** (§2.1). ~10 lines in one helper, and it removes a
   whole class of "later failures print nothing" reports — plus the latent
   deadlock above the pipe buffer.
3. **Give the pipeline an HTTP-client seam and delete the
   `http.DefaultTransport` patching** (§2.2). Unblocks testing the GitHub
   backend path properly, makes §4.1 easy, and is a prerequisite for ever
   adding `t.Parallel()`.
4. **Test `cleanupOldVersions` and add the missing pre-flight rejections**
   (§4.8, §4.2, §4.3, §4.4). Four small tests that kill five surviving mutants
   between them, on the destructive path (`cleanupOldVersions` deletes files)
   and the guards that produce paq's *useful* error messages.
5. **Extract `internal/testutil` and split data tests from loader tests**
   (§3.1, §2.5). Removes ~15 duplicated helpers and stops routine
   registry-data commits from breaking `internal/config`. Do this fifth, not
   first: it is the change that makes the other four cheaper next time, but it
   fixes no defect by itself.

Platform coverage is tracked separately, in §8. Its step 1 — making the test
isolation helpers platform-complete — is a prerequisite for enabling the CI
matrix and should land before any Windows or macOS test is written, because
today those helpers would make a Windows run write to the real user profile
rather than to a temp dir.

---

## 6. Mutation testing — executed, not simulated

26 mutants were applied one at a time to a scratch copy of the tree, each
followed by a full `go test ./...`. **9 were killed, 17 survived.** A survivor
means: that change to production code leaves the entire suite green.

Harness: copy the tree, apply a single textual substitution, run `go test ./...`,
revert. Every result below is reproducible. All of it ran on Linux, so the
platform-constrained tests of §8 were part of the suite the mutants faced;
on windows five of them are not built, which can only make the score worse.

### Survivors (17)

| # | Mutation | File | Why the suite misses it | Missing test |
|---|---|---|---|---|
| M13 | Skip the checksum's minisign verification entirely (`if false`) | `internal/install/pipeline.go:394` | **No pipeline test ever configures a valid minisign spec.** `verify.CheckMinisign` is well tested in isolation; its only production call site in the install path is not. And the plan passed to `verify.Run` blanks the minisign fields (lines 429-430), so nothing re-checks it downstream. | §4.1 — all three |
| M20 | `cleanupOldVersions` keep-set → empty map | `cmd/paq/upgrade.go:220` | `cleanupOldVersions` is at 0 % coverage. `TestRunUninstallKeepsSharedDest` covers the *uninstall* keep-set, not the upgrade one. | §4.8 |
| M26 | Drop the "custom registry url requires public_key" check | `cmd/paq/registry_update.go:197` | `TestRegistryUpdateRequiresPublicKey` asserts only `err != nil`, and the call reaches the network, so a later failure satisfies it. | §1.3 |
| M3 | Drop the `SupportsPlatform` pre-flight | `internal/install/pipeline.go:148` | No pipeline test uses a spec that excludes the running platform. | §4.2 |
| M4 | Drop the `extract` + `binaries` mutual-exclusion check | `internal/install/pipeline.go:227` | No spec in any test sets both. | §4.3 |
| M25 | Drop `runUpgrade`'s multi-arg pre-validation loop | `cmd/paq/upgrade.go:55` | The test asserts only that *an* error comes back; with the loop gone the batch fails anyway. | §1.4 |
| M2 | `hooks.IgnoreLock = true` → `false` in `upgradeApp` | `cmd/paq/upgrade.go:153` | `upgrade`'s tests all stop before `install.Run`; `IgnoreLock` is tested at the pipeline level (`lock_test.go:99`) but nothing checks that `upgrade` sets it. With it false, `paq upgrade` silently no-ops for every locked app. | `TestUpgradeAppIgnoresTheLockfile`: lock pins 1.0.0, upstream has 2.0.0, assert 2.0.0 installed |
| M1 | `resolvedLive := false` → `true` | `internal/install/pipeline.go:169` | `TestPipelineFixedVersionDoesNotWriteLock` passes because `TracksLatest` is false, and `TestPipelineUsesLockedVersionInsteadOfLatest` never asserts the lockfile is *unchanged* after reusing it. | Assert the lockfile's mtime/content is untouched when a lock entry was reused |
| M5 | `CheckFile` drops `strings.ToLower` on the expected digest | `internal/verify/sha256.go:109` | No fixture supplies an uppercase digest to `CheckFile`. The sha512 test's comment claims to cover this but its fixture is lowercase. | `TestCheckFileAcceptsUppercaseDigest` for both widths |
| M6 | `InstallFile` skips `chmod` | `internal/install/file.go:370` | `internal/install` has no `file_test.go`; `TestPipelineInstallFile` asserts content, never mode. | `TestInstallFileAppliesChmod` (0755 → mode check), plus invalid-chmod case |
| M7 | `writeFile` ignores the entry mode, always 0755 | `internal/archive/archive.go:116` | No archive test asserts an extracted file's permissions. | `TestExtractPreservesEntryMode`: 0644 file stays 0644, 0755 stays 0755 |
| M8 | `writeFile` drops the final `Chmod` | `internal/archive/archive.go:131` | Same gap. `TestInstallBinaries` checks 0755 but `InstallBinaries` chmods separately. | Same as M7 |
| M14 | `securePath` drops the lexical traversal check | `internal/archive/archive.go:102` | `os.Root` still blocks the escape, so the traversal tests still see an error and an absent file — they assert neither the message nor which layer refused. Defence-in-depth is unverified. | Assert the error names the offending entry: `illegal path %q in archive` |
| M15 | Drop the upfront `Content-Length > maxBytes` rejection | `internal/download/download.go:76` | `TestToTempLimitedRejectsOversizeWithContentLength` passes via the streaming check. The fast path (reject before reading a byte) is untested. | Assert the error text distinguishes the two: `(Content-Length %d)` |
| M17 | Drop the prerelease filter in the minimum-age scan | `internal/version/github_release.go:86` | The only prerelease fixture (`v2.9.0`) is *also* too new, so the age filter already excludes it. | `TestGitHubReleaseProviderSkipsOldPrerelease`: an old prerelease must be skipped in favour of an older stable release |
| M19 | Age cutoff `!PublishedAt.After(cutoff)` → `PublishedAt.Before(cutoff)` | `internal/version/github_release.go:89` | No fixture sits exactly on the cutoff. | `TestGitHubReleaseProviderAcceptsReleaseExactlyAtCutoff` with `published_at == now-MinimumAge` |
| M9 | `runParallel` no longer sorts the failure list | `cmd/paq/install.go:187` | `TestRunParallelRunsEveryAppDespiteFailures` has one failure, so ordering is unobservable. The sort exists to make the summary deterministic. | Two failing apps, assert the summary lists them alphabetically |

### Killed (9) — what the suite does defend well

| # | Mutation | Killed by |
|---|---|---|
| M10 | `State.Save` drops `s.sort()` | `TestSaveOrdersRecordsDeterministically` |
| M11 | `State.Delete` ignores the empty-version wildcard | `TestMultipleVersionsCoexist` |
| M12 | `httpretry.maxAttempts` 3 → 1 | 3 tests, incl. `TestDoGivesUpAfterMaxAttempts` (which deliberately asserts the literal 3) |
| M16 | Off-by-one in the streaming byte limit (`n > maxBytes+1`) | `TestToTempLimitedRejectsOversizeWithoutContentLength` |
| M18 | Drop the draft filter in the minimum-age scan | `TestGitHubReleaseProviderMinimumAge` |
| M21 | Drop the root-directory guard in `checkRemovableDir` | `TestCheckRemovableDirRefusesRoot` |
| M22 | Drop the unowned-dest guard in the pipeline | `TestPipelineRefusesToReplaceUnownedDest` |
| M23 | Drop `runInstall`'s multi-arg pre-validation | `TestRunInstallMultiArgFailsFastOnUnknownName` |
| M24 | `ToTemp` accepts non-200 responses | `TestToTempRejectsNon200` (all 3 sub-cases) |

**Reading of the result.** The mutants were chosen where gaps were suspected, so
17/26 is not a project-wide mutation score. What it does show is a clear
pattern: **destructive and verifying code paths are defended where a test was
written specifically for the regression** (M21, M22, M24 — all guarded by tests
whose comments name the incident) **and undefended where coverage came for free
through a happy-path test** (M3, M4, M13, M20). The suite's weak spots are not
random; they are exactly the branches no one wrote a test *for*.

---

## 7. Architecture of the test strategy

### 7.1 Distribution across the pyramid

| Layer | Definition here | Tests | Share |
|---|---|---|---|
| **Unit** (pure functions, no I/O) | `version`, `template`, `jsonpath`, `platform`, `pathenv`, `config` types/dest/verify, `ui.colWidths`, `exitcode`, `errors`, `completion` helpers | ~120 | 36 % |
| **Integration, in-process** (real FS + `httptest`, one package) | `archive`, `install`, `download`, `httpretry`, `state`, `registry`, `verify`, `updatecheck`, config load/write/lock | ~145 | 44 % |
| **Command-level** (`runX` called directly, captured stdout) | `cmd/paq` command tests | ~64 | 19 % |
| **End-to-end** (real binary, real network) | `e2e/e2e_test.go` | 1 | 0.3 % |

The shape is healthy — a broad unit base, a thick integration middle, a thin
command layer — with three structural caveats:

- **The "command level" is not the CLI.** Every test calls `runInstall`,
  `runDoctor`, etc. directly, bypassing `rootCmd.Execute`,
  `PersistentPreRunE` wiring, argument parsing, `reportError` (0 %) and the
  process exit code. `exitCodeFor` is well tested as a function; nothing
  verifies that `paq` the *process* exits 4 on a tamper. For a CLI whose
  documented contract includes exit codes and `--json` output, that is the
  gap that matters most at this layer.
- **E2E is one test behind two gates** (`//go:build e2e` *and*
  `if: github.event_name == 'workflow_dispatch'` in `.github/workflows/ci.yml`),
  so in practice it runs when someone remembers. That is a defensible cost
  decision — it hits the real GitHub API — but it means the only test of the
  real binary is effectively not part of CI.
- **The pyramid is one OS deep.** Every number above is a Linux number: CI runs
  the suite on `ubuntu-latest` only, while releases cross-compile for darwin and
  windows too. §8 treats that as its own dimension.

### 7.2 Over- and under-tested domains

Measured as tests per 100 lines of production code, cross-referenced with the
mutation results:

**Heavily tested (and rightly so):**

- `internal/verify` — 31 tests / 90.4 % statements. The security core. Correctly
  the densest area, with error-identity assertions (`ErrVerification`) rather
  than message matching.
- `internal/archive` — 26 tests / 75.1 %, including 8 dedicated to path
  traversal, symlink escape, symlink chains, hardlinks and special entries.
  This is the untrusted-input boundary and it shows.
- `internal/install` — 42 tests. The pipeline is the program.

**Under-tested relative to blast radius:**

| Area | Evidence | Why it matters |
|---|---|---|
| `internal/ui` | 1 real test; renderers verified only by `strings.Contains` from `cmd/paq` (§2.3) | It is 100 % of what the user sees. `formatBinaries` at 22 % of statements. |
| Destructive paths | `cleanupOldVersions` 0 %, `removeRecordFiles` 40 %, `printUninstallTargets` 0 % | These delete user files. `uninstall` has good guard tests; `upgrade`'s cleanup has none (M20). |
| `self-update` | `replaceExecutable` 0 %, `runSelfUpdate` 56 % | It overwrites the running binary. The download/verify half is tested; the replace half is not. |
| `import` | `runImport` 0 % (only helpers tested) | It writes the user's manifest. |
| `registry status` | 45.7 %, `metaSource` and `humanAge` at 0 % | Low blast radius — acceptable. |
| Shell completion | `completeManifestApps`, `completeInstallableNames` 0 % | Low blast radius — acceptable. |

**The pattern:** verification and parsing are well covered; **mutation and
deletion of user state is not**. Of the four functions that delete files
(`removeRecordFiles`, `cleanupOldVersions`, `InstallDir`'s swap,
`replaceExecutable`), two have dedicated safety tests and two have none.

### 7.3 Happy path vs failure path

Failure-path coverage is, unusually, a *strength* of this suite. Counting
assertions that require an error:

- `internal/verify`: 17 of 31 tests assert a failure — mismatches, wrong keys,
  tampered content, malformed documents, and (importantly) the distinction
  between "did not match" and "could not be checked".
- `internal/archive`: 9 of 26 assert a rejection, and several also assert the
  attack artifact was *not left on disk*.
- `internal/download`: 4 of 6 are failure paths.
- `internal/install`: 12 of 42.

Where the failure path is thin:

1. **No I/O failure injection except via path tricks.** The suite's one
   technique is "point an env var at a file where a directory is expected"
   (`pipeline_test.go:1176`) — clever, and it caught a real regression. But
   disk-full, permission-denied mid-write, a truncated HTTP body and a
   partially-written archive are all untested. `writeFile`'s explicit
   non-deferred `Close()` (`archive.go:127`) exists precisely to surface a
   flush failure, and nothing tests it.
2. **No cancellation path anywhere.** `ctx` threads through the pipeline, the
   backends and every provider; no test cancels one. Ctrl-C during a parallel
   install is a documented behaviour (`install.go:143`) with zero coverage.
3. **No concurrency failure path.** `TestConcurrentUpdate` (`state_test.go:114`)
   proves records are not lost across 10 goroutines — good. But
   `runParallel`'s limit of 3 is never exceeded in a test (the one test uses
   exactly 3 names), and the `stdoutMu` serialisation that exists to stop
   interleaved output is never verified.
4. **Command layer skews happy.** `ls`, `info`, `config show`, `registry show`
   each have a JSON test and a human test on a *valid* fixture; none covers a
   populated-but-broken state (a record with an empty `Kind`, a `Files` list
   with a missing entry, a spec whose template cannot resolve).

### 7.4 Proposed strategy — same cost, better placed

The suite is already fast (2.7 s) and cheap to maintain in the places that
matter. The proposal is a **reallocation**, not an expansion: retire ~350 lines
of redundant/data-coupled tests (§3) and spend the same budget here.

1. **Add one real CLI-level test file** (`cmd/paq/cli_test.go`), 5–8 cases,
   invoking the built binary via `exec.Command` with an isolated `HOME`:
   `paq --version` exits 0; `paq nosuchcommand` exits 2; a sha256 mismatch
   against a local `httptest` server exits 4; `paq ls --json` on an empty state
   prints `[]` and exits 0; `paq install --json` is rejected. This closes the
   §7.1 gap (exit codes, `reportError`, `Execute`) with one file and no network.
   Use `testing.M` + `go build -o` once, so the cost is ~1 s.
2. **Move rendering assertions down into `internal/ui`** as golden-output tests
   (§2.3), and reduce the `cmd/paq` output tests to behaviour: which entries,
   which JSON keys, which exit code. This *lowers* maintenance — a column-width
   tweak then touches one package instead of six test files.
3. **Introduce two seams and stop patching globals**: an `HTTPClient` on the
   install pipeline (§2.2) and a `clock func() time.Time` on
   `GitHubReleaseProvider` (killing M19's fixture-margin problem). Two struct
   fields; they unlock §4.1 and make the age tests exact instead of
   approximate.
4. **Add a failure-injection helper** to `internal/testutil`: an
   `io.Writer`/`http.Handler` that fails after N bytes. Four tests then cover
   truncated downloads, mid-extraction write failure, a failing `Close`, and a
   body shorter than its `Content-Length` — the whole class named in §7.3.1 for
   roughly 60 lines.
5. **Add one fuzz target** for the checksum-file parser
   (`FuzzParseSHA256File`) and one for `jsonpath.Select`. Both are pure
   string→(value,error) functions over untrusted input, both already have a
   clear invariant ("never return a value alongside an error"), and Go's
   built-in fuzzing costs nothing in CI when run with the default
   `-fuzz` disabled (the seed corpus runs as a normal test).
6. **Keep e2e gated, but widen it slightly and schedule it.** One `workflow_dispatch`
   test that only checks an exit code is not worth its complexity. Make it
   assert the version (§1.1), add a second case for a `url`-backend tool with a
   published checksum, and run it nightly on `main` rather than on request. The
   failure mode e2e exists to catch — a real publisher changing their asset
   layout — happens on their schedule, not on ours.

**Net maintenance impact:** fewer test files coupled to registry data, fewer
duplicated helpers, no monkey-patched globals, three new mechanisms
(CLI harness, failure injection, fuzz) that future tests reuse. Wall time stays
under ~5 s.

---

## 8. Platform coverage

`paq` is cross-compiled for six targets (`.sdlc/cross`: linux, darwin and
windows, each amd64 and arm64) and its test job runs on exactly one of them —
`.github/workflows/ci.yml` pins `runs-on: ubuntu-latest`. Two of the three
shipped operating systems have never run a single test.

That has two consequences. Every platform-specific branch in production code is
unverified on the platform that owns it (§8.4). And tests that quietly depend on
Linux behaviour were indistinguishable from portable ones, because the
dependency was expressed as a runtime `t.Skip` — which reports as a pass.

### 8.1 Convention

Platform-dependent tests carry a build constraint instead of a runtime skip:

- a `_linux_test.go` / `_windows_test.go` / `_darwin_test.go` filename, whose
  suffix Go already treats as a constraint, plus an explicit `//go:build` line
  so the constraint is visible in the file and greppable across the tree;
- `_unix_test.go` for the Unix-wide cases, where the explicit `//go:build
  !windows` is load-bearing (`unix` is not a filename suffix Go recognises —
  the same reason `internal/pathenv/pathenv_other.go` carries its tag while
  `pathenv_windows.go` relies on its name).

A file-level header comment states *what* about the platform the tests depend
on, so the reader knows what an equivalent on another OS has to assert.

One hazard of carrying both: a file renamed from `_linux_test.go` to
`_windows_test.go` whose `//go:build linux` line is not updated silently stops
building everywhere. When copying one of these files to another platform,
change the tag and the name together.

A runtime skip is never the right tool here. `go test` prints a skip as a pass,
so the two env-mapping tests below spent their life reporting green on every
platform while running on none but Linux.

### 8.2 Tests constrained today, and what the other platforms owe

| File | Constraint | Tests | Depends on | Owed elsewhere |
|---|---|---|---|---|
| `internal/install/env_mapping_linux_test.go` | `linux` | `TestPipelineAppliesEnvMapping`, `TestPipelineAppliesEnvArchMapping` | `platform.Detect` only ever sets `Env` on Linux (`"gnu"`); the whole point of `[x.env]` / `[x.env_arch]` is remapping that value | darwin and windows: `{{env}}` resolves to `""` there, so the equivalent asserts that an `[x.env]` map keyed for another platform is ignored and leaves no stray token in the URL |
| `internal/install/binaries_unix_test.go` | `!windows` | `TestInstallBinariesAppliesChmod` | Unix permission bits: `os.Chmod` on Windows only toggles the read-only flag, so `Mode().Perm()` never reports the spec's `chmod` | windows: assert the installed file is executable in the sense Windows has — present, not read-only, named with `{{ext}}` = `.exe` — and that a `chmod` in the spec is a no-op rather than an error |
| `internal/archive/symlink_unix_test.go` | `!windows` | `TestExtractTarGzSymlink`, `TestExtractTarGzSymlinkAbsoluteTargetRejected`, `TestExtractTarGzSymlinkChainEscapeRejected`, `TestExtractTarGzDanglingSymlinkAllowed` | creating a symlink needs a privilege the Windows runner does not have by default, and `/etc/passwd` is not an absolute path on Windows so the absolute-target refusal cannot be triggered the same way | windows: the absolute-target case with a `C:\…` target, and a decision on the unspecified behaviour below |

The last row hides a product question, not just a test gap: on Windows
`root.Symlink` fails without the privilege, so **an archive containing a symlink
cannot be installed at all**, and nothing says so. `internal/archive/tar.go`
materialises symlinks unconditionally; the `vscode` recipe already excludes
darwin for a related reason (its `.app` bundle holds 14 symlinks the zip
extractor refuses), documented in `embedded/registry/vscode.toml`. Whether
Windows should skip symlink entries, fail with a clear message, or require the
privilege is undecided — and it will stay undecided until a test on Windows
forces the answer.

The symlink tests that never touch the filesystem stay portable and run
everywhere: the zip extractor's refusals (`TestExtractZipSymlink*`, decided from
the zip header's mode bits), the lexically-escaping target
(`TestExtractTarGzSymlinkEscapingTargetRejected`, refused by `securePath`
before any write), and a wanted entry that turns out to be a symlink
(`TestExtractTarGzExtractNamedSymlinkReported`).

### 8.3 Two tests were made portable instead of constrained

`os.UserHomeDir` reads `$HOME` on Unix and `%USERPROFILE%` on Windows, so a test
that sets only `HOME` does not redirect the home directory on Windows — it would
fail there for a reason that has nothing to do with what it asserts. Both now
set `USERPROFILE` alongside `HOME`:

- `cmd/paq/uninstall_test.go` `TestRemoveRecordFilesRefusesHomeDir` — the guard
  it tests compares `dest` against the resolved home, so an unredirected home
  makes the comparison miss and the refusal never fire.
- `cmd/paq/root_test.go` `TestApplyConfigPathOverride` — asserts `~` expansion
  lands inside the temp home.

`TestCheckRemovableDirRefusesWithoutHome` (`uninstall_test.go`) and
`TestExpandHomeFailsWithoutHome` (`internal/install/pipeline_test.go`) already
cleared both variables and needed no change. Preferring a portability fix over a
build constraint is the rule: constrain a test only when the *behaviour* it
asserts is platform-specific, not when the fixture is.

### 8.4 Blocker: the isolation helpers do not isolate on Windows

This must be fixed **before** the CI matrix is switched on, because the failure
mode is not a red build. `config.userConfigPath` reads `APPDATA` on Windows, and
`state.StatePath`, `registry.Dir` and `updatecheck.Path` read `LOCALAPPDATA`;
the helpers below set only the XDG variables, which Windows ignores. A Windows
run would therefore read — and **write** — the real user's manifest, lockfile
and state file.

| Helper / test | Sets | Missing on Windows |
|---|---|---|
| `doctorEnv` (`cmd/paq/doctor_test.go:14`), used by ~20 tests | `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME` | `APPDATA`, `LOCALAPPDATA` |
| `isolateState` (`internal/install/pipeline_test.go:66`), used by ~25 tests | `XDG_STATE_HOME` | `LOCALAPPDATA` |
| `internal/config/write_test.go` (4 tests) | `XDG_CONFIG_HOME` | `APPDATA` |
| `cmd/paq/install_test.go`, `upgrade_test.go`, `init_test.go` | `XDG_CONFIG_HOME` | `APPDATA` |
| `cmd/paq/update_notify_test.go` `openGate` | `XDG_CACHE_HOME`, `XDG_CONFIG_HOME` | `APPDATA`, `LOCALAPPDATA` |

The pattern to copy already exists in the same tree: `setupEnv`
(`cmd/paq/registry_update_test.go:122`) and `corruptCache`
(`cmd/paq/registry_offline_test.go:12`) both set the Windows variables under a
`runtime.GOOS == "windows"` check, and `internal/registry`'s `setCacheHome` and
`internal/updatecheck`'s `TestPathPerOS` do the same. Where a helper only needs
the manifest, `config.PathOverride` is better still: it is OS-independent, and
`internal/config/lock_test.go`'s `isolateManifest` and
`internal/install/lock_test.go`'s `isolateConfig` already use it.

### 8.5 Production behaviour with no test on the platform that owns it

| Code | Platform | State |
|---|---|---|
| `internal/pathenv/pathenv_windows.go` `AddToUserPath` | windows only | 0 %. Writes `HKCU\Environment` and broadcasts `WM_SETTINGCHANGE`. The **only** item in this table that genuinely cannot be tested from another OS |
| `internal/pathenv/pathenv_other.go` `AddToUserPath` | !windows | 0 %. Returns the "only supported on Windows" error — one assertion away from covered |
| `internal/pathenv/pathenv.go` `listContains` | shared, Windows semantics | Tested, but only ever executed on Linux: it parses `;`-separated, case-insensitive, quote- and backslash-tolerant Windows PATH entries |
| `internal/state/process_windows.go` `processAlive` | windows only | Never runs. The Unix twin is covered by `TestUpdateReclaimsStaleLock` / `TestUpdateFailsWhenLockedByAnotherProcess`, which are the tests that decide whether a stale lock blocks every state write |
| `cmd/paq/update_notify_windows.go` / `update_notify_unix.go` `spawnDetached` | per-OS | Both 0 % |
| `StatePath`, `registry.Dir`, `updatecheck.Path`, `config.userConfigPath` — the `APPDATA`/`LOCALAPPDATA` arms | windows only | Only `updatecheck.Path` and `registry.Dir` have a Windows arm asserted (`TestPathPerOS`, `TestDir`), and only by building the expected string, never by running there |
| `platform.Detect` darwin/windows arms | per-OS | `TestDetect` is written the right way — a `switch d.OS` with per-OS expectations, so it becomes meaningful the moment it runs elsewhere. Only the linux arm has ever executed |
| `{{ext}}` → `.exe` through a real install | windows only | `TestResolveExt` covers the template substitution; no install test ever produces a `.exe` |
| `[<tool>.windows]` recipe overrides (zip instead of tar.gz, `.exe` assets) | windows only | Asserted as *data* by `TestWindowsArchiveOverride`, `TestMicroSpec`, `TestVSCodeSpec` (§2.5); no install on Windows ever exercises the resulting zip path |

### 8.6 Plan

1. **Make the isolation helpers platform-complete** (§8.4). Mechanical, small,
   and a hard prerequisite: without it the first Windows CI run writes to the
   runner's real profile instead of a temp dir.
2. **Add the OS matrix to the `test` job** —
   `strategy.matrix.os: [ubuntu-latest, macos-latest, windows-latest]`, with
   `runs-on: ${{ matrix.os }}`. Leave `cross-build` and the gated `e2e` job as
   they are. The suite runs in ~4 s, so the matrix costs runner minutes, not
   wall time.
3. **Treat the first matrix run's failures as findings, not as tests to skip.**
   Every red test is either a real platform bug or a fixture that assumed
   Unix; both are worth knowing. Adding a `t.Skip` to get green would recreate
   exactly the blind spot this section exists to remove.
4. **Write the owed equivalents** from §8.2, and the two one-assertion gaps from
   §8.5 (`pathenv_other.AddToUserPath`, and `listContains` is already fine).
5. **Accept one permanent gap**: `AddToUserPath` on Windows touches the user's
   registry and broadcasts a window message. It needs either a Windows CI job
   willing to write to `HKCU` under a temp key, or a seam that lets the test
   substitute the registry accessor. Until then it stays uncovered, knowingly.

Steps 1–3 are worth more than steps 4–5: they turn "we do not know" into a list.

---

## Appendix — verification log

| Claim | How it was verified |
|---|---|
| 74.0 % statement coverage; 19 functions at 0 % | `go test ./... -coverpkg=./... -coverprofile=...` + `go tool cover -func` |
| Suite runs in 2.7 s / ~4 s with `-race -shuffle=on`, stable | `time go test ./...`; `./.sdlc/test` equivalent run 3× |
| `captureStdout` leaks `os.Stdout` on in-capture `t.Fatal` | Standalone reproduction of the helper; the following test observes `os.Stdout.Fd() == 9` |
| `captureStdout` deadlocks above the pipe buffer | Same reproduction with a 200 KiB write; test binary hits its timeout inside `os.File.Write` |
| 26 mutants, 9 killed, 17 survived | Each mutant applied to a scratch copy of the tree, followed by a full `go test ./...`, then reverted |
| 0 benchmarks, 0 fuzz targets, 0 `t.Parallel()` | `grep -rn 'func Benchmark\|func Fuzz\|t.Parallel()' --include='*_test.go' .` |
| Helper duplication counts | `grep -rn 'RoundTrip(req \*http.Request)\|^func make\|^func sha256\|^func newSigner' --include='*_test.go' .` |
| Which pipeline guards are never entered | Per-block aggregation of the `-coverpkg` profile: `grep 'install/pipeline.go' cover.out \| awk '{c[$1]+=$3} END {for (k in c) print c[k], k}'` |
| The platform-constrained files build for their target, and the rest still build for all three | `for os in linux darwin windows; do GOOS=$os go vet ./...; done` — `go vet` type-checks test files, so a wrong build tag shows up here |
| Tests built per OS: 330 / 328 / 323 | `go test ./... -list '.*'` on linux, minus the `Test*` functions in the `linux`- and `!windows`-constrained files |
| Which test files each OS compiles | `for os in linux darwin windows; do GOOS=$os go list -f '{{.ImportPath}}: {{.TestGoFiles}}' ./internal/install ./internal/archive; done` — `env_mapping_linux_test.go` appears only for linux, `*_unix_test.go` only for linux and darwin |
