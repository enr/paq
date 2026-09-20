# paq — Problem Detection audit

Does a failure surface immediately, or can it pass unnoticed?

Snapshot of branch `claude/audit-features-docs-i7dih0` at `cc7f24e`.
Findings marked **[verified]** were reproduced against a binary built from
this tree; the rest are code-level readings.

---

## 1. Violations — where an error passes unnoticed

Ordered by severity. "Swallowed" means the error value exists and is discarded.

| # | Location | What is lost | Severity |
|---|---|---|---|
| M2 | `internal/httpretry/httpretry.go:29` | retries are invisible, even under `--debug` | **Media** |
| M3 | `internal/install/pipeline.go:490` | "Installed ✓" printed *before* the state save that can fail | **Media** |
| M4 | `cmd/paq/info.go:64` | `ResolveVars` error dropped in a diagnostic command | **Media** |
| B1 | `internal/ui/table.go` ×6, `which.go:58`, `import.go:98`, `registry_status.go:98` | `json.MarshalIndent` error → empty output, exit 0 | **Bassa** |
| B2 | `cmd/paq/update_check.go:32,39` | background check failure is undiagnosable by design | **Bassa** |
| B3 | `cmd/paq/registry_update.go:150` | dropped error silently skips downgrade protection | **Bassa** |

---

## 2. Risks, in detail

### M2 — Retries are completely invisible

`httpretry.Do` retries up to 3 times with backoff and honours `Retry-After`,
emitting nothing: no hook, no callback, no output even under `--debug` (the
package deliberately has no `ui` dependency).

A user on a flaky link sees a command hang ~3s, then ~6s, with no hint why.
Sustained 429s — the signal that you are being rate-limited and should set
`GITHUB_TOKEN` — are invisible until the final failure. A degrading condition
is unobservable right up to the point where it breaks.

### M3 — Success is announced before the operation that can still fail

```go
ok(fmt.Sprintf("Installed %s %s → %s", appName, ver, dest))   // :490
// 13. Record in the state DB
if err := state.Update(…); err != nil {
    return fmt.Errorf("save state: %w", err)                  // :506
}
```

If the state write fails the user sees `✓ Installed rg … → /path` immediately
followed by `✗ save state: …`. The error *is* returned (good), but the two
messages contradict each other, and the real outcome is the confusing one: the
binary **is** on disk and **is not** tracked, so `ls`, `upgrade` and
`uninstall` will not see it. That is M1's drift, created by paq itself.

### M4 — `info` discards its own resolution error

```go
resolvedSpec, vars, _ := install.ResolveVars(cfg, plat, spec, app)   // :64
```

`paq info` is what a user runs to debug why a recipe misbehaves. If resolution
fails, it prints the spec with unresolved placeholders and no indication that
anything went wrong — the exact moment the user most needs the error.

### B1 — JSON marshal errors dropped (8 sites)

```go
data, _ := json.MarshalIndent(packages, "", "  ")
fmt.Println(string(data))
```

On error this prints an empty line and returns success, so `paq ls --json | jq`
gets empty input with exit 0. In practice these are plain structs of strings,
times and slices, so `MarshalIndent` effectively cannot fail — **low severity,
listed for the pattern rather than the bug**.

### B2 — The background update check cannot be diagnosed

`__update-check` returns `nil` on lookup failure and does `_ = st.Save()`, while
`spawnDetached` points its stdio at `os.DevNull`. If the cache directory is
read-only or full, the update check silently never works again, and no flag —
`--debug` included — can reveal it. Defensible for a non-essential nag, but
currently *undiagnosable*, which is a different thing from *quiet*.

### B3 — Dropped error skips the downgrade guard

```go
if _, cur, _ := registry.Open(); cur != nil && !force {
```

A corrupt cache makes `Open` return an error and a nil meta, so the
downgrade/no-op protection is skipped silently. Low impact (the update then
overwrites the bad cache, which is the desired recovery) but the skip is
invisible.

---

## 3. Concrete improvements

**A1 — make `doctor` fail loudly.** Report the error instead of skipping, parse
the config rather than stat-ing it, and let the exit code carry the verdict:

```go
cfg, err := loadConfig()
if err != nil {
    ui.WarnField("Config", "unusable", "("+err.Error()+")")
    ui.Hint("fix the manifest, or run `paq init --force` to start from a skeleton")
    problems++
} else {
    … existing block …
}
…
if problems > 0 {
    return fmt.Errorf("%d problem(s) found", problems)
}
```

Counting `problems` across every check (config, state, registry, PATH) also
turns `doctor` into a usable CI gate. Worth adding a `doctor --json` for
scripted health checks.

**A2 — replace substring matching with typed errors.** Define a sentinel in
`internal/verify` and wrap at the source:

```go
// internal/verify/verify.go
var ErrVerification = errors.New("verification failed")
// sha256.go:31
return fmt.Errorf("%w: sha256 mismatch for %s: …", ErrVerification, …)
```

then `exitCodeFor` becomes `errors.Is(err, verify.ErrVerification)`, which
survives any rewording and works through `%w` wrapping and the `runParallel`
aggregate. Keep the substring check as a fallback for one release if you like,
but **add a test that drives a real mismatch through `verify.Run` and asserts
`exitCodeFor` returns 4** — that is the test that is missing today.

**A3 — check `Close` on both write paths.** In `writeFile`, drop the `defer`
in favour of an explicit close before the chmod:

```go
if _, err := io.Copy(f, r); err != nil {
    f.Close()
    return fmt.Errorf("write %s: %w", name, err)
}
if err := f.Close(); err != nil {
    return fmt.Errorf("close %s: %w", name, err)
}
return root.Chmod(name, mode&0777|0200)
```

and the same in `installRawBinary` before the chmod/rename. Consider
`f.Sync()` before `Close` on the final artifact if durability across a crash
matters.

**M1 — add a reconciliation check.** Cheapest useful version: a `paq doctor`
section that stats every recorded path and reports the missing ones, counted
into the exit code above:

```
! State drift:   2 recorded tool(s) missing on disk
                 rg@14.1.1 → /home/u/.local/bin/rg
```

Optionally mark them in `paq ls` (a `MISSING` status column) and let
`which` exit non-zero for a recorded-but-absent path, which is what a script
actually needs.

**M2 — give `httpretry` an optional callback.** Keeps the leaf package free of
`ui`:

```go
type Options struct { OnRetry func(attempt int, status int, delay time.Duration, err error) }
```

Wire it to `ui.Debug` normally, and to `ui.Warn` on a 429 with the
`GITHUB_TOKEN` hint — a rate limit is exactly the condition a user can act on.

**M3 — move the success message after the state write**, or downgrade it to a
step and emit `ok(...)` once the record is durable.

**M4 — stop discarding the error**; `info` should report it and still print
what it can:

```go
resolvedSpec, vars, err := install.ResolveVars(cfg, plat, spec, app)
if err != nil {
    ui.Warn("placeholder resolution failed (%v): showing the raw spec", err)
} else {
    spec = resolvedSpec
}
```

**B1 — handle the marshal error** (`if err != nil { return fmt.Errorf(...) }`)
rather than printing an empty document. Mechanical, 8 sites.

**B2 — record the outcome instead of discarding it.** Persist
`LastError`/`LastAttempt` in the update-check state and surface it in `doctor`
("update check: last succeeded 14 days ago"). The check stays silent; it stops
being undiagnosable.

**B4 — machine-readable errors.** There is no telemetry, which is right for a
local CLI — but today a `--json` consumer gets human text on stderr and an exit
code derived from string matching (A2). Emitting a JSON error envelope on
stdout in `--json` mode (`{"error": {"kind": "verification", "message": …}}`)
would give scripts a reliable signal and make A2's fragility moot.
