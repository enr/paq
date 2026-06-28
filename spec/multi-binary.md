# Design — Archivi multi-binary

Compagno di `spec/mvp.md`. Definisce una terza modalità di installazione per gli
archivi che contengono **più eseguibili**, da installare in una *bin directory*
(es. `~/.local/bin`) invece che come cartella in `~/.local/opt`.

Stato: **implementato**.

---

## 1. Problema

Un archivio come `zipp-<ver>_<os>_<arch>.zip` contiene tre eseguibili
(`zipts`, `zipls`, `zipw`) sotto una dir di primo livello. Con il modello attuale
l'unica scelta è installarlo come **directory**:

```toml
[zipp]
backend = "github"
repo = "enr/zipp"
asset = "zipp-{{version}}_{{os}}_{{arch}}.zip"
archive = "zip"
strip_components = 1
```

Risultato: i tre binari finiscono in `~/.local/opt/zipp/` — non sul `PATH`.
L'utente li vorrebbe come eseguibili singoli in `~/.local/bin/`.

### 1.1 Modello attuale (due modalità)

Decise in `internal/install/pipeline.go` (~riga 344):

| Modalità | Trigger ricetta            | `dest`               | Esempio   |
|----------|----------------------------|----------------------|-----------|
| `file`   | `extract = "rg{{ext}}"`    | path di un **file**  | ripgrep   |
| `dir`    | nessun `extract`           | path di una **dir**  | jdk, zipp |

Manca la modalità "estrai N binari nominati e mettili in una bin dir".

---

## 2. Soluzione: modalità `binaries`

Nuovo campo di ricetta `binaries`: lista di eseguibili da estrarre dall'archivio
e installare in `dest`, interpretato come **directory** (la bin dir). Ogni binario
riceve il `chmod` della ricetta.

`binaries` è **mutuamente esclusivo** con `extract`:

- `extract` valorizzato → modalità `file` (un singolo binario, `dest` = file).
- `binaries` valorizzato → modalità `binaries` (`dest` = directory bin).
- nessuno dei due → modalità `dir` (come oggi).

`strip_components` e `subdir` continuano a valere per localizzare i file dentro
l'archivio (i nomi in `binaries` sono confrontati per **basename** dopo lo strip,
coerentemente con il comportamento attuale di `extract`).

### 2.1 Schema di configurazione (con rename)

`binaries` è una lista di tabelle con due campi:

- `from` (obbligatorio): basename dell'eseguibile nell'archivio (templated).
- `to` (opzionale): nome con cui installarlo in `dest`; se omesso, usa il
  basename di `from`.

Si usa una lista di tabelle (non una lista mista stringhe/tabelle) per avere un
array TOML omogeneo e un parsing prevedibile.

**Ricetta** (`embedded/registry/zipp.toml`):

```toml
[zipp]
backend = "github"
repo = "enr/zipp"
asset = "zipp-{{version}}_{{os}}_{{arch}}.zip"
archive = "zip"
strip_components = 1
chmod = "0755"
binaries = [
  { from = "zipts{{ext}}" },
  { from = "zipls{{ext}}" },
  { from = "zipw{{ext}}" },
]
```

**Con rename** (es. evitare collisioni sul `PATH`):

```toml
binaries = [
  { from = "zipw{{ext}}", to = "zipwatch{{ext}}" },
]
```

**Manifest utente** (`~/.config/paq/config.toml`):

```toml
[apps.zipp]
use = "zipp"
version = "latest"
dest = "~/.local/bin"
```

Risultato: `~/.local/bin/zipts`, `~/.local/bin/zipls`, `~/.local/bin/zipw`
(o `zipwatch` con il rename).

`{{ext}}` resta lo strumento per la differenza Windows (`.exe`); non serve un
override `binaries` per-OS.

### 2.2 Download nudo (senza archivio)

`binaries` è utilizzabile **anche senza archivio**: se `archive` è omesso,
l'artefatto scaricato è direttamente l'eseguibile. Copre i tool che pubblicano un
binario nudo con os/arch/versione nel nome, da installare con un nome pulito.

```toml
[mytool]
backend = "github"
repo = "owner/mytool"
asset = "mytool_{{version}}_{{os}}_{{arch}}{{ext}}"
chmod = "0755"
binaries = [ { to = "mytool{{ext}}" } ]
```

Regole del caso senza archivio:

- è ammessa **una sola** entry (un download = un file);
- `from` è ignorato (non c'è nulla da estrarre); `to` è il nome installato.
  Se `to` è vuoto, il default è il basename dell'asset scaricato;
- l'artefatto viene copiato in `dest/<to>` con swap atomico nella bin dir
  (l'artefatto può stare su un filesystem diverso da `dest`).

---

## 3. Punti di intervento

### 3.1 `internal/config/types.go`

Aggiungere il tipo e il campo:

```go
// Binary è un eseguibile da estrarre da un archivio multi-binary.
// To, se vuoto, vale come il basename di From.
type Binary struct {
    From string `toml:"from"`
    To   string `toml:"to"`
}

type Recipe struct {
    // ...
    Binaries []Binary `toml:"binaries"`
    // ...
}
```

Eventuale supporto in `RecipeOverride` per-OS: **fuori scope** (coperto da `{{ext}}`).

Il parsing in `internal/config/load.go` (`parseRecipeFile`) re-encoda la mappa
"pulita" in TOML e la decodifica in `Recipe`: una lista di tabelle `binaries`
viene gestita dal round-trip esistente senza codice speciale. Da verificare con
un test di load dedicato.

### 3.2 Pipeline — `internal/install/pipeline.go`

Nel blocco install (~riga 344), aggiungere il ramo prima di `dir`:

```go
switch {
case recipe.Extract != "":
    kind = "file"
    // ... come ora

case len(recipe.Binaries) > 0:
    kind = "binaries"
    // risolvi i template di From/To per ogni binario
    // chiama install.InstallBinaries(...)

default:
    kind = "dir"
    // ... come ora
}
```

Per ogni `Binary`:

1. Risolvi `From` e `To` con `template.Resolve` (gestisce `{{ext}}`, ecc.).
2. Se `To` è vuoto → `To = filepath.Base(From)`.

Il `chmod` segue la precedenza già esistente: `app.Chmod` sovrascrive
`recipe.Chmod`.

### 3.3 `internal/install` — nuova `InstallBinaries`

Firma proposta:

```go
func InstallBinaries(
    archivePath, archiveType string,
    bins []ResolvedBinary,         // {From, To} già risolti
    destDir, chmod string,
    opts archive.ExtractOpts,      // StripComponents, Subdir
) (installed []string, err error)
```

Comportamento (atomico per-binario, riusa la logica di `InstallFile`):

0. Se `archiveType == ""` (download nudo): una sola entry ammessa, l'artefatto
   viene copiato in `destDir/<To>` con swap atomico nella bin dir e `chmod`.
1. `os.MkdirAll(destDir, 0755)`.
2. Estrai **tutti** i binari in una temp dir nella stessa dir di `destDir`
   (stesso filesystem → rename atomico). Riuso del meccanismo esistente:
   un giro di `archive.Extract` per ogni `From` con `ExtractOpts.Extract=From`.
   Archivi piccoli ⇒ riaprire l'archivio N volte è accettabile e **non richiede
   modifiche al package `archive`**.
3. `chmod` su ciascun estratto.
4. Rename atomico di ognuno in `destDir/<To>`.
5. Ritorna la lista dei path installati (assoluti) per lo state.

> Variante (non raccomandata ora): estendere `archive.ExtractOpts` con un set di
> basename per estrarre in una passata sola. Più invasivo; rimandato a quando il
> costo di riapertura diventasse misurabile.

### 3.4 State — `internal/state/state.go`

`AppRecord` oggi traccia un solo `Dest` + `Kind` (`file`/`dir`). Per `binaries`
serve la lista dei file installati:

```go
type AppRecord struct {
    // ... invariati
    Kind  string   `json:"kind"`            // "file" | "dir" | "binaries"
    Dest  string   `json:"dest"`            // per "binaries": la bin dir
    Files []string `json:"files,omitempty"` // popolato solo per "binaries"
}
```

`Files` è opzionale (`omitempty`) ⇒ retrocompatibile: i record `file`/`dir`
esistenti restano validi e non richiedono migrazione. Lo `schemaVersion`
**non** va bumpato (aggiunta additiva di campo opzionale).

La pipeline registra `Kind: "binaries"`, `Dest: <bin dir>`, `Files: installed`.

### 3.5 Uninstall — `cmd/paq/uninstall.go`

In `removeRecordFiles` aggiungere il case:

```go
case "binaries":
    for _, p := range rec.Files {
        if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
            return fmt.Errorf("remove %s: %w", p, err)
        }
    }
```

Non si rimuove `Dest` (è una bin dir condivisa, es. `~/.local/bin`): si rimuovono
solo i file installati da questa app.

### 3.6 Import — `cmd/paq/import.go`

`defaultDest` (~riga 138) deve riconoscere la nuova modalità:

```go
func defaultDest(recipe config.Recipe, key string) string {
    switch {
    case recipe.Extract != "":
        return "~/.local/bin/" + recipe.Extract
    case len(recipe.Binaries) > 0:
        return "~/.local/bin"          // directory
    default:
        return "~/.local/opt/" + key
    }
}
```

### 3.7 Validazione

In fase di load/merge (o all'inizio della pipeline) segnalare errore se una
ricetta ha **sia** `extract` **sia** `binaries`: sono modalità incompatibili.
Messaggio chiaro, in inglese (coerente con la UI esistente).

### 3.8 Docs

- `README.md`: aggiungere la terza modalità nella sezione "Adding a custom recipe"
  con l'esempio `binaries` (e la variante rename).
- `docs/content/docs/`: stessa nota.

---

## 4. Compatibilità

- **Ricette esistenti**: nessun cambiamento (campo nuovo, default vuoto).
- **State esistente**: nessuna migrazione (campo `files` additivo opzionale).
- **zipp**: la ricetta passa da modalità `dir` a `binaries`; gli utenti che
  l'avevano installata in `~/.local/opt/zipp` dovranno reinstallare. Da citare
  nelle note di release.

---

## 5. Piano di test

- **config**: load di una ricetta con `binaries` (con e senza `to`); errore se
  `extract` + `binaries` coesistono.
- **install**: `InstallBinaries` su uno zip e su un tar.gz con più entry; verifica
  che i file finiscano in `destDir/<to>` con il `chmod` corretto; rename applicato.
- **pipeline**: end-to-end con un archivio fixture multi-binary; `kind="binaries"`,
  `Files` popolato nello state.
- **uninstall**: rimuove solo i file elencati, lascia intatta la bin dir.
- **import**: `defaultDest` ritorna `~/.local/bin` per ricette `binaries`.
- **e2e**: caso `zipp` reale (se la rete in CI lo consente, come per gli altri).

---

## 6. Decisioni

- **Rename supportato** via `{from, to}` (richiesto). `to` opzionale.
- **`binaries` come lista di tabelle** (non lista mista) per array TOML omogeneo.
- **Loop per-binario** sull'estrazione: zero modifiche a `internal/archive`.
- **`binaries` per-OS**: fuori scope (coperto da `{{ext}}`).
- **schemaVersion invariato**: `files` è additivo/opzionale.
