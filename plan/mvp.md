# Implementation Plan — paq v0.0.1

## Stato attuale
Repository vuoto. Solo `spec/mvp.md` presente. Da costruire da zero.

---

## M0 — Scaffolding

### Step 0.1 — Inizializza il modulo Go [DONE]

**Cosa fare:**
```bash
cd /home/user/paq
go mod init github.com/enr/paq
```

Crea `go.mod` con il nome del modulo. Nessun codice ancora.

**Verifica:** `cat go.mod` mostra `module github.com/enr/paq` e la versione Go.

---

### Step 0.2 — Installa le dipendenze [DONE]

**Cosa fare:**
```bash
go get github.com/pelletier/go-toml/v2
go get github.com/ulikunitz/xz
go get github.com/jedisct1/go-minisign
go get github.com/spf13/cobra
go get github.com/charmbracelet/lipgloss
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/bubbles
go get github.com/charmbracelet/log
```

**Verifica:** `go.sum` e `go.mod` aggiornati con tutte le dipendenze.

---

### Step 0.3 — Crea la struttura delle directory [DONE]

**Cosa fare:** crea le seguenti directory vuote (con `.gitkeep`):
```
cmd/paq/
internal/platform/
internal/config/
internal/template/
internal/version/
internal/backend/
internal/download/
internal/verify/
internal/archive/
internal/install/
internal/state/
internal/ui/
embedded/registry/
```

**Verifica:** `find . -type d | grep -v .git` mostra tutte le directory.

---

### Step 0.4 — Crea l'entrypoint e i 4 comandi cobra (no-op) [DONE]

**File da creare:** `cmd/paq/main.go`
```go
package main

func main() {
    Execute()
}
```

**File da creare:** `cmd/paq/root.go`
- Definisce `rootCmd` con cobra
- Flag globali: `--no-color`, `--json`, `-q` (quiet), `-v` (verbose)
- Funzione `Execute()` che chiama `rootCmd.Execute()`

**File da creare:** `cmd/paq/install.go`
- Comando `install [app]` — per ora stampa `"not implemented"` e ritorna

**File da creare:** `cmd/paq/ls.go`
- Comando `ls` — per ora stampa `"not implemented"`

**File da creare:** `cmd/paq/uninstall.go`
- Comando `uninstall <app>` — per ora stampa `"not implemented"`

**File da creare:** `cmd/paq/info.go`
- Comando `info <app>` — per ora stampa `"not implemented"`

**Verifica:**
```bash
go run ./cmd/paq --help
go run ./cmd/paq install --help
go run ./cmd/paq completion bash
go run ./cmd/paq completion zsh
```
Tutti devono funzionare senza errori.

---

### Step 0.5 — Aggiungi versione e Makefile [DONE]

**File da creare:** `cmd/paq/version.go`
- Variabile `var Version = "0.0.1-dev"`
- Comando `version` che stampa la versione

**File da creare:** `Makefile` con questi target:
```makefile
build:
    go build ./cmd/paq

test:
    go test ./...

lint:
    go vet ./...

cross:  # compila i 6 target
    GOOS=linux   GOARCH=amd64 go build -o dist/paq-linux-amd64   ./cmd/paq
    GOOS=linux   GOARCH=arm64 go build -o dist/paq-linux-arm64   ./cmd/paq
    GOOS=darwin  GOARCH=amd64 go build -o dist/paq-darwin-amd64  ./cmd/paq
    GOOS=darwin  GOARCH=arm64 go build -o dist/paq-darwin-arm64  ./cmd/paq
    GOOS=windows GOARCH=amd64 go build -o dist/paq-windows-amd64.exe ./cmd/paq
    GOOS=windows GOARCH=arm64 go build -o dist/paq-windows-arm64.exe ./cmd/paq
```

**Verifica:** `make build` produce un binario, `make cross` produce i 6 file in `dist/`.

---

### Step 0.6 — Aggiungi CI GitHub Actions [DONE]

**File da creare:** `.github/workflows/ci.yml`
- Trigger: `push` e `pull_request`
- Job `test`: `go test ./...`
- Job `cross-build`: `make cross`, upload artifacts

**Verifica:** push su branch, CI verde.

---

## M1 — Platform + Template

### Step 1.1 — Package `internal/platform` [DONE]

**File da creare:** `internal/platform/platform.go`

Deve esporre:
```go
// Defaults contiene i valori di piattaforma risolti
type Defaults struct {
    OS     string // "linux", "darwin", "windows"
    Arch   string // "amd64", "arm64"
    Vendor string // "unknown" su linux, "apple" su darwin, "pc" su windows
    Env    string // "gnu" su linux amd64/arm64, "musl" ecc (default "gnu")
    Ext    string // "" su linux/darwin, ".exe" su windows
}

// Detect ritorna i defaults per la piattaforma corrente
func Detect() Defaults
```

**Logica:**
- `OS`: usa `runtime.GOOS` → mappa diretta
- `Arch`: usa `runtime.GOARCH` → mappa diretta
- `Vendor`: se OS==darwin → "apple"; se OS==windows → "pc"; altrimenti → "unknown"
- `Env`: se OS==linux → "gnu"; altrimenti → ""
- `Ext`: se OS==windows → ".exe"; altrimenti → ""

**Test da scrivere:** `internal/platform/platform_test.go`
- Testa che su linux/amd64 i valori siano corretti
- Testa la funzione `ApplyOSMap(m map[string]string, os string) string` che prende una mappa `[x.os]` e ritorna il valore overridato se presente, altrimenti il default

---

### Step 1.2 — Package `internal/template` [DONE]

**File da creare:** `internal/template/template.go`

Deve esporre:
```go
type Vars struct {
    OS            string
    Arch          string
    Vendor        string
    Env           string
    Ext           string
    Version       string
    VersionMajor  string
    VersionMinor  string
    VersionPatch  string
    // Named templates (es. rust_target) vengono aggiunti dinamicamente
    Extra         map[string]string
}

// Resolve sostituisce tutti i {{placeholder}} in s con i valori in v.
// Ritorna errore se un placeholder non è riconosciuto.
func Resolve(s string, v Vars) (string, error)
```

**Logica di `Resolve`:**
- Cerca tutti i pattern `{{nome}}` con `regexp`
- Per ogni nome, cerca prima in `v.Extra`, poi nei campi struct (case insensitive: `version`, `version_major`, `os`, `arch`, `vendor`, `env`, `ext`)
- Se non trovato → `error`

**File da creare:** `internal/template/metatemplate.go`

```go
// MetaTemplates contiene template nominati definiti in templates.toml
type MetaTemplates map[string]string  // es. "rust_target" → "{{arch}}-{{vendor}}-{{os}}-{{env}}"

// Expand espande i meta-template: per ogni chiave in mt, la aggiunge a v.Extra
// espandendo il suo valore con le var correnti. Supporta override per-OS.
func Expand(mt MetaTemplates, osOverrides map[string]MetaTemplates, v Vars) (Vars, error)
```

**Logica di `Expand`:**
1. Parti da `v`
2. Per ogni entry in `mt`: risolvi il valore con `Resolve(value, v)` → aggiungi a `v.Extra[key]`
3. Se esiste un override per `v.OS` in `osOverrides`, applica sopra (stesso processo)
4. Ritorna la `v` arricchita

**Test da scrivere:** `internal/template/template_test.go`
- `Resolve("{{arch}}-{{vendor}}-{{os}}-{{env}}", ...)` → `"x86_64-unknown-linux-gnu"`
- `Resolve("rg{{ext}}", ...)` → `"rg"` su linux, `"rg.exe"` su windows
- `Resolve("{{unknown}}", ...)` → errore
- Meta-template `rust_target` su linux → `"aarch64-unknown-linux-gnu"` ecc
- Meta-template `rust_target` su darwin → `"aarch64-apple-darwin"` (senza env)

---

### Step 1.3 — Versione: parsing lasco [DONE]

**File da creare:** `internal/version/clean.go`

```go
// Clean rimuove prefisso "v" e suffissi non numerici da una stringa di versione
// Esempi: "v14.1.1" → "14.1.1", "jdk-21.0.2+13" → "21.0.2"
func Clean(raw string) string

// Parse estrae major/minor/patch da una stringa già pulita
// Fallisce silenziosamente (ritorna "" per i campi mancanti)
func Parse(version string) (major, minor, patch string)
```

**Test:** `"v14.1.1"` → `"14.1.1"`, major=`"14"`, minor=`"1"`, patch=`"1"`.

---

## M2 — Config + Merge

### Step 2.1 — Strutture dati config [DONE]

**File da creare:** `internal/config/types.go`

```go
// Recipe è una ricetta della registry (un tool installabile)
type Recipe struct {
    Backend         string
    Repo            string
    Asset           string
    Source          string
    Archive         string
    Extract         string
    Subdir          string
    StripComponents int
    Chmod           string
    OS              map[string]string
    Arch            map[string]string
    Env             map[string]string
    Templates       map[string]string
    TemplatesOS     map[string]map[string]string
    Verify          VerifyConfig
}

type VerifyConfig struct {
    SHA256      string
    SHA256Asset string
    Minisign    MinisignConfig
}

type MinisignConfig struct {
    PublicKey   string
    SignedAsset string
}

// AppEntry è la configurazione di un'app nel manifest utente
type AppEntry struct {
    Use     string
    Version string
    Dest    string
    OS      map[string]string
    Arch    map[string]string
    Env     map[string]string
    Chmod   string
}

// Config è la configurazione completa post-merge
type Config struct {
    Recipes map[string]Recipe
    Apps    map[string]AppEntry
}
```

---

### Step 2.2 — Embedded registry e parsing TOML [DONE]

**File da creare:** `embedded/registry/templates.toml`
**File da creare:** `embedded/registry/ripgrep.toml`
**File da creare:** `embedded/registry/jdk.toml`
**File da creare:** `embedded/embedded.go`
**File da creare:** `internal/config/load.go`

Funzioni:
```go
func LoadEmbeddedRegistry(fs embed.FS) (map[string]Recipe, error)
func LoadUserConfig() (*Config, error)
func Merge(embedded map[string]Recipe, user *Config) (*Config, error)
```

---

### Step 2.3 — Comando `paq info` (sola lettura) [DONE]

**Modifica:** `cmd/paq/info.go`

Il comando deve:
1. Caricare la config con `config.Merge(...)`
2. Trovare l'app nel manifest (`Config.Apps[appName]`)
3. Trovare la ricetta corrispondente (`Config.Recipes[app.Use]`)
4. Stampare in modo leggibile: backend, repo/source, asset/extract, verify, dest, version

---

## M3 — Versione + Backend

### Step 3.1 — Provider versione `pin` [DONE]

**File da creare:** `internal/version/provider.go`

```go
type Provider interface {
    Resolve(ctx context.Context) (version string, tag string, err error)
}

type PinProvider struct {
    Version string
}
```

---

### Step 3.2 — Provider versione `github_release` (latest) [DONE]

**File da creare:** `internal/version/github_release.go`

```go
type GitHubReleaseProvider struct {
    Repo       string
    HTTPClient *http.Client
}
// GET https://api.github.com/repos/{repo}/releases/latest
// Parsea JSON: {"tag_name": "v14.1.1", ...}
```

---

### Step 3.3 — Backend `url` [DONE]

**File da creare:** `internal/backend/url.go`

```go
type URLBackend struct {
    Source string
}
func (b URLBackend) Resolve(v template.Vars) (string, error)
```

---

### Step 3.4 — Backend `github` [DONE]

**File da creare:** `internal/backend/github.go`

```go
type GitHubBackend struct {
    Repo       string
    Asset      string
    HTTPClient *http.Client
}
func (b GitHubBackend) Resolve(ctx context.Context, tag string, v template.Vars) (string, error)
// GET /repos/{repo}/releases/tags/{tag}, cerca asset per nome esatto
```

---

## M4 — Download + Archive

### Step 4.1 — Download con progress reader [DONE]

**File da creare:** `internal/download/download.go`

```go
type ProgressFn func(downloaded, total int64)
func ToTemp(ctx context.Context, client *http.Client, url string, progress ProgressFn) (path string, err error)
```

---

### Step 4.2 — Estrazione `tar.gz` [DONE]

**File da creare:** `internal/archive/targz.go`

```go
type ExtractOpts struct {
    StripComponents int
    Extract         string
    Subdir          string
    Dest            string
}
func ExtractTarGz(r io.Reader, opts ExtractOpts) error
```

---

### Step 4.3 — Estrazione `tar.xz` [DONE]

**File da creare:** `internal/archive/tarxz.go`
Refactorizza logica comune in `internal/archive/tar.go`.

---

### Step 4.4 — Estrazione `zip` [DONE]

**File da creare:** `internal/archive/zip.go`

```go
func ExtractZip(path string, opts ExtractOpts) error
```

---

### Step 4.5 — Dispatcher archivi [DONE]

**File da creare:** `internal/archive/archive.go`

```go
func Extract(archivePath string, archiveType string, opts ExtractOpts) error
```

---

## M5 — Verify

### Step 5.1 — Verifica SHA256 [DONE]

**File da creare:** `internal/verify/sha256.go`

```go
func CheckFile(filePath string, expected string) error
func ParseSHA256File(checksumPath string, fileName string) (string, error)
```

---

### Step 5.2 — Verifica minisign [DONE]

**File da creare:** `internal/verify/minisign.go`

```go
func CheckMinisign(filePath, signaturePath, pubKeyBase64 string) error
```

---

### Step 5.3 — Pipeline di verifica [DONE]

**File da creare:** `internal/verify/verify.go`

```go
type Plan struct {
    SHA256Literal   string
    SHA256AssetPath string
    ArtifactName    string
    MinisignPubKey  string
    MinisignSigPath string
    ArtifactPath    string
}
func Run(plan Plan) error
// ordine: 1) verifica firma minisign del checksum, 2) verifica sha256 artefatto
```

---

## M6 — Install + State

### Step 6.1 — State DB [DONE]

**File da creare:** `internal/state/state.go`

Formato lista (convenzione lockfile), identità `(Name, Version)` per supportare più versioni
della stessa app coesistenti.

```go
type AppRecord struct {
    Name        string    `json:"name"`
    Version     string    `json:"version"`
    Kind        string    `json:"kind"`
    Dest        string    `json:"dest"`
    Source      string    `json:"source"`
    SHA256      string    `json:"sha256"`
    InstalledAt time.Time `json:"installed_at"`
}

type State struct {
    Schema   int         `json:"schema"`
    Packages []AppRecord `json:"packages"`
}

func StatePath() (string, error)
func Load() (*State, error)
func (s *State) Save() error
func (s *State) Set(rec AppRecord)              // upsert per (Name, Version)
func (s *State) Get(name, version string) (AppRecord, bool)
func (s *State) ByName(name string) []AppRecord
func (s *State) Delete(name, version string) int // version=="" → rimuove tutte
```

---

### Step 6.2 — Install: file singolo [DONE]

**File da creare:** `internal/install/file.go`

```go
func InstallFile(archivePath, archiveType, extractName, dest, chmod string) error
// 1. MkdirAll dest dir
// 2. Estrai in temp nella stessa dir di dest
// 3. Applica chmod
// 4. os.Rename atomico su dest
```

---

### Step 6.3 — Install: directory [DONE]

**File da creare:** `internal/install/dir.go`

```go
func InstallDir(archivePath, archiveType, dest string, opts archive.ExtractOpts) error
// 1. Estrai in dest.tmp
// 2. dest → dest.bak
// 3. dest.tmp → dest
// 4. rimuovi dest.bak
```

---

### Step 6.4 — Pipeline completa `paq install` [DONE]

**File da creare:** `internal/install/pipeline.go`

```go
func Run(ctx context.Context, cfg *config.Config, appName string, progressFn download.ProgressFn) error
```

Segui esattamente i passi della sezione 6 della spec. Regola: se qualsiasi passo fallisce, dest resta intatto.

---

### Step 6.5 — Comandi `ls` e `uninstall` [DONE]

**Modifica:** `cmd/paq/ls.go` — carica state, stampa tabella
**Modifica:** `cmd/paq/uninstall.go` — rimuove file/dir, cancella entry state
**Modifica:** `cmd/paq/install.go` — chiama `install.Run`

---

## M7 — UX

### Step 7.1 — Package `internal/ui` base [DONE]

**File da creare:** `internal/ui/ui.go`

```go
type Config struct {
    NoColor bool
    JSON    bool
    Quiet   bool
    Verbose bool
}
var Global Config
func IsTTY() bool
```

---

### Step 7.2 — Barra di progresso download [DONE]

**File da creare:** `internal/ui/progress.go`

```go
func NewProgressFn(label string) download.ProgressFn
// usa bubbles/progress su stderr; fallback testuale se non TTY
```

---

### Step 7.3 — Tabelle `ls` e `info` con lipgloss [DONE]

**File da creare:** `internal/ui/table.go`

```go
func PrintLsTable(packages []state.AppRecord)
func PrintInfoDetail(name string, recipe config.Recipe, app config.AppEntry, installed []state.AppRecord)
// se JSON: serializza su stdout
```

---

### Step 7.4 — Output step `install` e esito verifica [DONE]

**File da creare:** `internal/ui/log.go`

```go
func Step(msg string, args ...any)
func OK(msg string, args ...any)
func Fail(msg string, args ...any)
func Info(msg string, args ...any)
// usa charmbracelet/log; rispetta --quiet e NO_COLOR
```

---

### Step 7.5 — Integrazione UX nella pipeline [DONE]

- `install.Run`: aggiunge chiamate `ui.Step/OK/Fail`
- `cmd/paq/ls.go`: chiama `ui.PrintLsTable`
- `cmd/paq/info.go`: chiama `ui.PrintInfoDetail`
- `cmd/paq/root.go`: imposta `ui.Global` da flag/env

---

### Step 7.6 — Completion e `--help` finali [DONE]

Verifica che `paq completion bash|zsh|fish|powershell` funzioni correttamente.

---

## M8 — Hardening + Demo

### Step 8.1 — Gestione errori completa [DONE]

- Exit code ≠ 0 su qualsiasi errore
- Messaggi chiari e descrittivi
- Nessun panic non gestito
- File temporanei sempre eliminati con `defer`

---

### Step 8.2 — Test di integrazione con httptest [DONE]

**File da creare:** `internal/install/pipeline_test.go`

- Simula API GitHub releases con `httptest`
- Serve `.tar.gz` e `.sha256` fake
- Esegui `install.Run` completo, verifica `dest`
- Caso checksum manomesso → errore, dest intatto

---

### Step 8.3 — Test E2E (gated, richiede rete) [DONE]

**File da creare:** `e2e/e2e_test.go` con `//go:build e2e`

```go
func TestInstallRipgrep(t *testing.T) { ... }
```

Attivato con `go test -tags=e2e ./e2e/`.

---

### Step 8.4 — Cross-build e CI finale [DONE]

**Modifica:** `.github/workflows/ci.yml`
- Job cross-build per i 6 target
- Upload artifacts
- Job E2E gated/manuale

---

### Step 8.5 — README [DONE]

**File da creare:** `README.md`
- Cosa è paq, come buildarlo, esempio config.toml, i 4 comandi, come aggiungere ricetta custom.

---

## Legenda
- `[ ]` = da fare
- `[DONE]` = completato
