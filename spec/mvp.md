# paq — Piano MVP v0.0.1

Compagno di `paq-spec.md`. Definisce cosa entra (e cosa no) nella prima release shippabile,
con architettura, modello dati, pipeline, milestone e criteri di accettazione.

Linguaggio assunto: **Go** (CGO disabilitato, binario statico). Riadattabile a Rust/Zig.

---

## 1. Obiettivo della 0.0.1

Installare end-to-end i due casi di riferimento, con il modello di configurazione completo e
verifica di sicurezza, tramite 4 comandi, su tutte le piattaforme target:

- **rg** (ripgrep): backend `github`, versione `latest`, archivio `.tar.gz` con dentro un singolo
  eseguibile → `dest` è un **file** (`~/bin/rg`).
- **jdk**: backend `url`, versione pinnata, archivio `.tar.gz` estratto come **directory**
  (`~/opt/jdk-{{version}}`).

"Funziona" = `paq install rg && rg --version` e `paq install jdk && <dest>/bin/java -version`
su Linux/macOS/Windows.

---

## 2. Scope

### In scope

- **Config**: registry embedded + registry utente + manifest utente, con deep-merge.
- **Backend**: `github` (release: risolve + match asset), `url` (templato).
- **Versione**: provider `pin` e `github_release` (strategia *latest*).
- **Template**: set completo di placeholder + meta-template nominati (globale + override per-OS).
- **Override**: mappe `os` / `arch` / `env` per app (nel blocco ricetta e/o manifest).
- **Verifica**: integrità `sha256` (literal pin, `sha256_asset`) + firma `minisign`, con ordine
  firma-su-checksum.
- **Estrazione**: `tar.gz`, `tar.xz`, `zip`; `strip_components`, `extract` (file singolo), `subdir` (sottoalbero).
- **Install**: file → rename atomico; directory → estrazione in temp + swap.
- **State**: state DB minimale (necessario per `ls`/`uninstall`/`info`).
- **Comandi**: `install`, `ls`, `uninstall`, `info`.
- **UX (prioritaria)**: cobra (help + completion bash/zsh/fish/powershell), output con lipgloss
  (tabelle `ls`/`info`, step di `install`, esito verifica colorato), barra di progresso download,
  e degradazione fuori da TTY (`NO_COLOR`/`--no-color`, `--json`, `-q`/`-v`; dati su stdout, progress
  su stderr).
- **Build multipiattaforma**: `{linux, darwin, windows} × {amd64, arm64}`.

### Fuori scope (rinviato)

- Multi-versione, store versionato, symlink/`current`, rollback, prune.
- Comando `update` (per ora `install` re-installa).
- Config di progetto (`./.config/paq/`), trust model, `{{cwd}}`/`{{project_dir}}`.
- Cache persistente e `--offline`.
- Lockfile dichiarativo.
- Vincoli semver range (`1.x`, `>=…`): solo `pin` + `latest`.
- Provider extra (`github_tag`, `git`, `json`, preset `aur`/`crates`/…).
- Firme `gpg` e `cosign`/sigstore (solo `minisign`).
- Mirror / fallback `source` multiplo; backend `github_artifact`.
- Match asset `glob`/`regex`/lista (solo `exact`).
- Gestione `PATH`; formati pacchetto distro (`.deb`/`.rpm`).
- Completion **dinamica** (nomi app/tool da contesto) e man page: in MVP completion statica via cobra.

---

## 3. Dipendenze

Volutamente minime:

- TOML: `github.com/pelletier/go-toml/v2`
- xz: `github.com/ulikunitz/xz` (per `tar.xz`; gzip/zip/tar dalla stdlib)
- minisign: `github.com/jedisct1/go-minisign` (verifica firma)
- HTTP e GitHub API: `net/http` (stdlib)
- CLI: `github.com/spf13/cobra` (comandi, help, completion)
- UI: `github.com/charmbracelet/lipgloss` (styling), `bubbletea` + `bubbles/progress` (barra di
  progresso download), `github.com/charmbracelet/log` (log/errori formattati)

Niente libreria semver: in MVP la versione `latest` arriva dall'endpoint *latest* di GitHub, e
`version_major/minor/patch` si derivano con un parser lasco interno.

---

## 4. Architettura (package layout)

```
cmd/paq/                   # entrypoint
  main.go
  root.go, install.go, ls.go, info.go, uninstall.go   # comandi cobra
internal/
  platform/                 # detect os/arch; default vendor/env; {{ext}}
  config/                   # caricamento + merge (embedded → user); structs
  template/                 # risoluzione placeholder + meta-template (cicli)
  version/                  # provider pin | github_release(latest); cleaning
  backend/                  # github, url → URL di download risolto
  download/                 # GET su file temporaneo (con progress reader)
  verify/                   # sha256, minisign; ordinamento firma→checksum
  archive/                  # tar.gz/tar.xz/zip; strip_components, extract, subdir
  install/                  # pipeline file/dir; rename atomico; swap dir
  state/                    # state.json read/write
  ui/                       # lipgloss styling, tabelle, progress, log; TTY detection, --json
embedded/
  registry/templates.toml   # meta-template globali
  registry/ripgrep.toml
  registry/jdk.toml
```

---

## 5. Modello dati

### Config (campi chiave, post-merge)

Ricetta (registry entry): `backend`, `repo`, `asset` | `source`, `archive`, `extract`,
`subdir`, `strip_components`, `chmod`, mappe `[x.os]`/`[x.arch]`, `env`, `[x.templates]`
(+ `[x.templates.<os>]`), `[x.verify]`.

Manifest (app): `use`, `version` (`latest` | pin), `dest`, override opzionali `os`/`arch`/`env`/`chmod`.

### State DB — `${XDG_STATE_HOME:-~/.local/state}/paq/state.json`

Lista di pacchetti installati (convenzione lockfile: `Cargo.lock`, `poetry.lock`, `Pipfile.lock`).
L'identità di una entry è la coppia `(name, version)`: questo consente di tracciare **più versioni
della stessa app** coesistenti su disco (es. `jdk` 21 e 26 in `dest` diversi), pur senza che paq
gestisca lo switch/`current` tra versioni (rinviato a 0.0.2+). Ogni entry porta con sé il proprio
`name` e `version`, quindi non c'è una chiave-nome che forzi una singola versione attiva.

```json
{
  "schema": 2,
  "packages": [
    {
      "name": "rg",
      "version": "14.1.1",
      "kind": "file",
      "dest": "/home/u/bin/rg",
      "source": "https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-x86_64-unknown-linux-gnu.tar.gz",
      "sha256": "…",
      "installed_at": "2026-06-22T10:00:00Z"
    },
    {
      "name": "jdk",
      "version": "21.0.2",
      "kind": "dir",
      "dest": "/home/u/opt/jdk-21.0.2",
      "source": "https://download.oracle.com/java/21/archive/jdk-21.0.2_linux-x64_bin.tar.gz",
      "sha256": "…",
      "installed_at": "2026-06-22T10:01:00Z"
    },
    {
      "name": "jdk",
      "version": "26",
      "kind": "dir",
      "dest": "/home/u/opt/jdk-26",
      "source": "https://download.oracle.com/java/26/archive/jdk-26_linux-x64_bin.tar.gz",
      "sha256": "…",
      "installed_at": "2026-06-22T10:02:00Z"
    }
  ]
}
```

Reinstallare la stessa `(name, version)` fa upsert (sostituzione in place); installare una versione
diversa **aggiunge** una entry invece di sovrascrivere, così i file della versione precedente non
restano orfani. `kind` (`file`|`dir`) + `dest` bastano a `uninstall` per rimuovere il giusto;
`uninstall <app>` accetta `<app>` o `<app>@<version>` per disambiguare quando coesistono più versioni.

---

## 6. Pipeline di install

1. **Risolvi**: provider versione (`pin` o `github_release` latest) → `version`/`tag`.
2. **Template**: applica mappe os/arch + default/override vendor/env → espandi meta-template →
   `asset` → URL di download (backend `github` o `url`).
3. **Download checksum/firma** (asset ausiliari risolti per nome templato).
4. **Verifica firma** del file checksum (`minisign`), se configurata — *prima* di scaricare l'artefatto.
5. **Download artefatto** su file temporaneo.
6. **Verifica integrità**: hash dell'artefatto contro il checksum.
7. **Installa**:
   - `dest` è un **file**: estrai/seleziona il singolo binario (`extract`), `chmod`, **rename atomico** su `dest`.
   - `dest` è una **directory**: estrai l'albero (`strip_components`/`subdir`) in `dest.tmp`, poi
     **swap** (`dest` → `dest.bak`; `dest.tmp` → `dest`; rimuovi `.bak`).
8. **Registra** nello state DB.

Regola di sicurezza transazionale: se un qualsiasi passo fallisce (verifica inclusa), `dest` resta
intatto. Su directory, lo swap avviene solo dopo verifica+estrazione riuscite.

> Caveat noto (accettato per MVP, conseguenza del "no symlink"): lo swap di directory ha una finestra
> non perfettamente atomica tra i due rename. L'atomicità piena via flip di symlink è 0.0.2+.

---

## 7. Comandi

- `paq install [app]` — installa una app (o tutte quelle del manifest). Risolve, verifica, installa,
  registra. Re-installa se la versione risolta differisce da quella nello state.
- `paq ls` — elenca le app installate dallo state DB: nome, versione, kind, dest.
- `paq uninstall <app>` — rimuove file/dir secondo lo state DB e cancella la entry.
- `paq info <app>` — mostra ricetta effettiva (post-merge), URL risolto e stato installato
  (versione, dest, source, sha256).

Exit code ≠ 0 su verifica fallita, asset non trovato, errore di rete.

---

## 8. Registry embedded e manifest di esempio

```toml
# embedded/registry/templates.toml
[templates]
rust_target = "{{arch}}-{{vendor}}-{{os}}-{{env}}"
[templates.darwin]
rust_target = "{{arch}}-{{vendor}}-{{os}}"        # apple: niente env
```

```toml
# embedded/registry/ripgrep.toml
[ripgrep]
backend = "github"
repo = "BurntSushi/ripgrep"
asset = "ripgrep-{{version}}-{{rust_target}}.tar.gz"
archive = "tar.gz"
extract = "rg{{ext}}"                              # singolo eseguibile → dest è un FILE
chmod = "0755"

[ripgrep.arch]
amd64 = "x86_64"
arm64 = "aarch64"

[ripgrep.verify]
sha256_asset = "{{asset}}.sha256"                 # da tarare sul layout reale dei checksum in fase di build
```

```toml
# embedded/registry/jdk.toml
[jdk]
backend = "url"
source = "https://download.oracle.com/java/{{version_major}}/archive/jdk-{{version}}_{{os}}-{{arch}}_bin.tar.gz"
archive = "tar.gz"
strip_components = 1                               # tarball linux: dir radice jdk-21.0.2/

[jdk.os]
darwin = "macos"

[jdk.arch]
amd64 = "x64"
arm64 = "aarch64"

# macOS ha layout diverso (jdk-21.jdk/Contents/Home): override per-OS che valida subdir
[jdk.darwin]
strip_components = 0
subdir = "*/Contents/Home"

[jdk.verify]
sha256 = "…"                                       # pin per versione (URL fisso)
```

```toml
# ~/.config/paq/config.toml  (manifest utente)
[apps.rg]
use = "ripgrep"
version = "latest"
dest = "~/bin/rg{{ext}}"

[apps.jdk]
use = "jdk"
version = "21.0.2"
dest = "~/opt/jdk-{{version}}"
```

---

## 9. Milestone / sequenza di lavoro

| # | Milestone | Deliverable |
|---|---|---|
| M0 | Scaffolding | repo, CLI skeleton con **cobra** (4 comandi no-op, `--help`/`completion`/`version`), build matrix in CI |
| M1 | Platform + template | detect os/arch, default vendor/env/`{{ext}}`, risoluzione placeholder + meta-template per-OS (unit test) |
| M2 | Config + merge | parsing TOML, embedded FS, merge embedded→user, structs ricetta/manifest, `paq info` (sola lettura) |
| M3 | Versione + backend | provider `pin`/`github_release(latest)`, cleaning; backend `github`/`url` → URL risolto |
| M4 | Download + archive | GET su temp con progress reader; estrazione tar.gz/tar.xz/zip con `strip_components`/`extract`/`subdir` |
| M5 | Verify | `sha256` (pin + asset) e `minisign`, con ordine firma→checksum |
| M6 | Install + state | pipeline file/dir, rename atomico, swap dir; state DB; `install`/`ls`/`uninstall` completi |
| M7 | UX | package `ui/`: lipgloss (tabelle `ls`/`info`, step `install`, esito verifica), barra di progresso, TTY detection, `--json`/`-q`/`-v`, completion |
| M8 | Hardening + demo | cross-build dei 6 target, e2e rg+jdk, gestione errori, README |

---

## 10. Criteri di accettazione

- `paq install rg` installa un `rg` funzionante e della versione *latest* corretta in `~/bin/rg`
  (su Windows `~/bin/rg.exe`); la verifica passa.
- `paq install jdk` installa la directory in `~/opt/jdk-21.0.2` e `<dest>/bin/java -version` funziona
  (incluso il caso macOS via `subdir`).
- Checksum manomesso → `install` fallisce con exit ≠ 0 e `dest` resta intatto.
- `paq ls` elenca rg e jdk con versione, kind e dest corretti.
- `paq info rg` mostra ricetta post-merge, URL risolto e stato installato.
- `paq uninstall jdk` rimuove la directory e la entry di stato; `ls` non la mostra più.
- `paq --help`, `paq install --help` e `paq completion bash|zsh|fish|powershell` funzionano.
- `paq ls --json | jq` funziona (dati su stdout); con output rediretto o `NO_COLOR` non compaiono
  codici colore/animazioni; il download mostra la barra di progresso su TTY.
- I 6 target compilano; gli eseguibili sulle piattaforme disponibili in CI girano.

---

## 11. Strategia di test

- **Unit**: risoluzione template (inclusi meta-template per-OS, `{{ext}}`, default `env`), cleaning
  versione (strip prefix/suffix, parse lasco), match asset esatto, verify (sha256 ok/ko, minisign
  valido/invalido, ordinamento), estrazione (fixture: tgz con binario annidato, zip, sottoalbero).
- **Integration**: server `httptest` che simula release + asset + `.sha256` (+ firma minisign con
  chiave di test); install completo in `dest` temporaneo, per file e per directory.
- **E2E smoke**: i due casi reali (rg, jdk), gated/manuali o in job CI con rete.

---

## 12. Note e decisioni pendenti

- **Linguaggio**: confermare Go (assunto qui).
- **Firma in MVP**: scelto `minisign` per semplicità in-process. Se un target di demo richiede
  `gpg`/`cosign`, è un cambio di scope.
- **Checksum ripgrep**: la riga `sha256_asset` va tarata sul layout reale dei checksum del progetto
  durante M5 (in alternativa `sha256 = "auto"` dal digest dell'API GitHub).
- **Swap directory non atomico**: accettato per MVP; l'atomicità piena (symlink flip) è 0.0.2+.

---

## 13. Anteprima 0.0.2 (non in questa release)

Cache + `--offline`, `update`, config di progetto + trust + `{{project_dir}}`, lockfile, vincoli
semver range, provider aggiuntivi e preset, firme `gpg`/`cosign`, mirror/fallback, match asset
glob/regex/lista. Lo store versionato + symlink (con rollback/prune) resta una decisione di scope
separata.
