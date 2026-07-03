# Implementation Plan — Registry esterno aggiornabile

Branch di lavoro: `claude/registry-extraction-design-c87nmy`

## Contesto

Il registry di paq (i recipe TOML dei tool installabili) è compilato nel binario
via `//go:embed` (`embedded/embedded.go` → `embedded/registry/*.toml`): per
aggiungere o correggere un recipe serve una release del binario. Obiettivo:
rendere il registry aggiornabile indipendentemente dal binario, mantenendo il
funzionamento offline e la sicurezza del canale di distribuzione.

### Decisioni prese

1. **Sorgente**: registry pubblicato come release asset del repo `enr/paq`
   (`registry.tar.gz` + `registry.tar.gz.sha256` + `registry.tar.gz.sha256.minisig`).
2. **Fallback**: il registry embedded resta nel binario; lo snapshot esterno in
   cache lo sovrascrive per nome. paq funziona sempre, anche offline al primo avvio.
3. **Aggiornamento**: solo esplicito via `paq registry update` — nessuna rete
   implicita negli altri comandi.
4. **Verifica**: sha256 + firma minisign del file checksum; chiave pubblica
   embedded nel binario come trust anchor. La verifica non è mai disattivabile.

### Architettura

```
<cache>/paq/registry/          # snapshot esterno = unità di swap atomico
    meta.json                  # {schema, tag, version, fetched_at, source_url, spec_count}
    registry/*.toml            # recipe + templates.toml (+ VERSION, scritto dalla CI)
```

- Cache dir: Linux/macOS `${XDG_CACHE_HOME:-~/.cache}/paq/registry`,
  Windows `%LOCALAPPDATA%\paq\cache\registry`.
- Precedenza: `embedded < snapshot esterno < [specs.*] utente`.
- `templates.toml` dello snapshot fa overlay **per chiave** (non replace) su
  quello embedded; se assente restano i template embedded.
- Versione del registry **intrinseca al contenuto**: file `registry/VERSION`
  dentro l'archivio (scritto dalla CI dal file `VERSION` del repo). Il registry
  embedded ha per definizione la versione del binario.
- Ogni `Spec` porta un campo runtime `Origin` (`embedded`/`registry`/`user`)
  per mostrare all'utente la provenienza di ogni definizione.
- Formato registry **append-only** (solo nuovi campi opzionali): un registry
  più nuovo si parsa su un binario più vecchio (go-toml ignora i campi ignoti).

---

## FASE 1 — Lato lettura [DONE — commit 506119c]

Implementato e testato (`go build ./... && go test ./...` verde):

- **`internal/registry/registry.go`** (nuovo): `Meta`, `Dir()`, `Open()`
  (`(nil,nil,nil)` se assente, errore se corrotto), `StagingDir()` (temp dir
  accanto allo snapshot, stesso filesystem), `Install(stagingDir, meta)` con
  swap atomico a tre passi (`registry → registry.old` → `staging → registry` →
  rimozione `.old`, con ripristino su errore).
- **`internal/registry/registry_test.go`** (nuovo): Dir con override env,
  Open su cache assente/corrotta, install fresh/replace, fallimento senza
  perdita dello snapshot precedente, nessun residuo `.old`/staging.
- **`internal/config/types.go`**: campo `Spec.Origin` (`toml:"-"`) + costanti
  `OriginEmbedded`/`OriginRegistry`/`OriginUser`.
- **`internal/config/load.go`**: `LoadEmbeddedRegistry` marca `OriginEmbedded`;
  `LoadUserConfig` marca `OriginUser`; nuova `OverlayRegistry(specs, global,
  globalOS, snapshotFS)` — applica lo snapshot in place (spec per nome con
  `OriginRegistry`, template globali per chiave, template per-OS per
  OS+chiave); su errore non modifica nulla; `templates.toml` mancante = ok,
  non parsabile = errore (scarta l'intero snapshot).
- **`cmd/paq/install.go`**: `loadConfig()` ora delega a `loadConfigWithMeta()`
  che apre lo snapshot via `registry.Open()`, fa l'overlay e ritorna anche il
  `*registry.Meta` in uso (nil = solo embedded). Cache rotta → `ui.Warn` su
  stderr (non corrompe la shell completion) e fallback a embedded; mai rete.
- **`internal/config/overlay_test.go`** (nuovo): test matrix del merge a tre
  livelli — collisioni su tutti i livelli (vince user, poi snapshot, poi
  embedded), spec solo-snapshot disponibile, replace integrale dello spec
  (niente merge campo-per-campo: verify/OSOverrides non "trapelano" dal
  livello sotto), template per chiave e per-OS, `Origin` corretto ovunque,
  snapshot corrotto (TOML invalido / dir registry mancante) scartato
  integralmente.
- **Rewording**: rimosso "embedded" da help/messaggi di `cmd/paq/registry.go`,
  `registry_list.go`, `registry_show.go`.

---

## FASE 0 — Azione una-tantum del maintainer (nessun codice) [TODO]

**Cosa fare:**
```bash
minisign -G -W    # genera keypair senza password
```
- La **chiave pubblica** (riga base64 di `minisign.pub`, es. `RWQ...`) va nel
  codice in Fase 2 (`internal/registry/trust.go`).
- La **chiave segreta** (contenuto di `minisign.key`) va nel secret GitHub
  Actions `MINISIGN_SECRET_KEY` del repo `enr/paq`.

Finché la chiave non è nel codice, `paq registry update` con sorgente di
default deve fallire con un errore chiaro (vedi Fase 2, guardia su
`DefaultPublicKey == ""`).

---

## FASE 2 — Lato scrittura: `paq registry update` [DONE]

Implementato e testato (`go build ./... && go vet ./... && go test ./...` verde):

- **`internal/registry/trust.go`** (nuovo): `const DefaultPublicKey = ""`
  (trust anchor; vuota finché il maintainer non esegue la Fase 0). L'update da
  sorgente di default si rifiuta di partire finché è vuota.
- **`internal/config/types.go` + `load.go`**: `RegistrySettings{URL, PublicKey}`
  (`toml:"registry"`), aggiunta a `userConfigRaw`, `Config`, al ritorno di
  `LoadUserConfig` e copiata da `Merge`.
- **`cmd/paq/registry_update.go`** (nuovo): comando `paq registry update` (flag
  `--force`), con `resolveRegistrySource` (default = release asset di
  `enr/paq` via `GitHubReleaseProvider`+`GitHubBackend`; custom = `[registry].url`
  solo https con `public_key` obbligatoria), download via `download.ToTemp`
  (client iniettabile `registryUpdateClient`), cap dimensione (`registryMaxBytes`,
  var per i test), `verify.Run` (minisign+sha256), estrazione post-firma in
  `registry.StagingDir()`, validazione pre-swap (specs>0, templates ok o
  assenti, `registry/VERSION` non vuoto), anti-downgrade con `version.Compare`
  e `--force`, `registry.Install`, report `ui.OK`.
- **`cmd/paq/registry_update_test.go`** (nuovo): firme minisign generate a
  runtime (ed25519 + `PrivateKey.Sign`, la libreria sa firmare — niente helper
  a mano), server `httptest.NewTLSServer` sul percorso url custom. Casi: happy
  path (spec visibile con origin `registry`), firma invalida (→ exit 4),
  tarball manomesso (→ exit 4), http rifiutato, url senza public_key,
  downgrade rifiutato / con `--force`, già aggiornato (no-op), archivio
  oversize, archivio senza recipe, archivio senza VERSION.

Nota: `exitcode.go` non ha richiesto modifiche — gli errori di `verify.Run`
contengono già le sottostringhe riconosciute (exit code 4 verificato dai test).

## FASE 2 (design originale) — Lato scrittura: `paq registry update`

### 2.1 Trust anchor — `internal/registry/trust.go` (nuovo)

```go
package registry

// DefaultPublicKey is the minisign public key that signs the official paq
// registry checksum file (base64, one-line format of minisign.pub).
// Generated by the maintainer; the secret key lives in the GitHub Actions
// secret MINISIGN_SECRET_KEY.
const DefaultPublicKey = "" // TODO(maintainer): set after Phase 0
```

### 2.2 Config surface — `internal/config/types.go` + `load.go`

```go
// RegistrySettings configures a custom source for "paq registry update"
// ([registry] table in config.toml). Setting URL requires PublicKey:
// the user explicitly replaces the default trust anchor.
type RegistrySettings struct {
    URL       string `toml:"url"`
    PublicKey string `toml:"public_key"`
}
```
- Aggiungere `Registry RegistrySettings` a `userConfigRaw`, a `Config` e al
  ritorno di `LoadUserConfig`; in `Merge` copiare `user.Registry` nel Config
  risultante.

### 2.3 Comando — `cmd/paq/registry_update.go` (nuovo)

Modellato su `cmd/paq/self_update.go` (`runSelfUpdate` è l'esempio completo di
resolve → download → verify → extract → swap). Registrarlo in `init()` con
`registryCmd.AddCommand(registryUpdateCmd)`. Unico flag: `--force` / `-f`.

Costanti: asset `registry.tar.gz`, checksum `registry.tar.gz.sha256`,
firma `registry.tar.gz.sha256.minisig`. Cap dimensione archivio: 10 MiB.

Flusso di `runRegistryUpdate`:

1. **Sorgente.** Leggere `[registry]` dal config utente
   (`config.LoadUserConfig()`; NON serve `loadConfig()` completo).
   - Se `url` è impostata: richiedere prefisso `https://` (errore altrimenti);
     richiedere `public_key` impostata (errore: `custom registry url requires
     public_key`). Gli URL da scaricare sono `url`, `url+".sha256"`,
     `url+".sha256.minisig"`. `tag` per la meta = `"custom"`.
   - Altrimenti (default): guardia `if registry.DefaultPublicKey == ""` →
     errore "this build has no registry signing key configured".
     Risolvere tag: `version.GitHubReleaseProvider{Repo: selfUpdateRepo}.Resolve(ctx)`
     (vedi self_update.go:52); risolvere i tre asset URL con
     `backend.GitHubBackend{Repo: selfUpdateRepo, Asset: <nome>}.Resolve(ctx, tag, vars)`
     (vars servono solo per il templating: basta `template.Vars{Version: latest}`).
2. **Download** dei tre file con `download.ToTemp(ctx, download.NewClient(),
   url, ui.NewProgressFn("registry"))` (progress solo per il tarball), con
   `defer os.Remove(...)`. Dopo il download del tarball: `os.Stat` e rifiuto
   se > 10 MiB.
3. **Verify** (riuso integrale, stessa catena di self-update ma con firma):
   ```go
   verify.Run(verify.Plan{
       SHA256AssetPath: shaPath,
       ArtifactName:    "registry.tar.gz",
       ArtifactPath:    tarPath,
       MinisignPubKey:  pubKey, // DefaultPublicKey o [registry].public_key
       MinisignSigPath: sigPath,
   })
   ```
   Il fallimento propaga l'errore così com'è: `cmd/paq/exitcode.go` riconosce
   già le sottostringhe "signature verification failed" / "integrity check" →
   exit code 4. (Verificato: nessuna modifica necessaria a exitcode.go.)
4. **Extract** in staging: `staging, _ := registry.StagingDir()` (+ `defer
   os.RemoveAll(staging)`); `archive.Extract(tarPath, "tar.gz",
   archive.ExtractOpts{Dest: staging})`. La protezione path-traversal
   (`securePath`) è già dentro `internal/archive`. L'estrazione avviene SOLO
   dopo la verifica della firma.
5. **Validazione pre-swap** (garantisce che la cache non possa mai finire in
   uno stato che `loadConfig` degraderebbe):
   - `specs, err := config.LoadEmbeddedRegistry(os.DirFS(staging))` → errore o
     `len(specs) == 0` → rifiuto ("registry archive contains no recipes");
   - `config.LoadGlobalTemplates(os.DirFS(staging))` → se l'errore non è
     `fs.ErrNotExist` → rifiuto;
   - leggere `staging/registry/VERSION` → assente o vuoto (dopo TrimSpace) →
     rifiuto ("registry archive has no VERSION").
6. **Anti-downgrade**: `_, cur, _ := registry.Open()`; se `cur != nil`:
   `cmp := version.Compare(version.Clean(newVersion), version.Clean(cur.Version))`;
   `cmp == 0` → `ui.OK("registry already up to date (%s)", newVersion)` e stop;
   `cmp < 0` → errore "refusing to downgrade registry from X to Y (use --force)".
   `--force` scavalca entrambi. (Best-effort: chi può scrivere nella cache dir
   possiede già l'account.)
7. **Swap**: `registry.Install(staging, registry.Meta{Tag: tag, Version:
   newVersion, FetchedAt: time.Now(), SourceURL: tarURL, SpecCount: len(specs)})`.
8. **Report**: `ui.OK("registry updated to %s (%d recipes)", newVersion, len(specs))`.

### 2.4 Test — `cmd/paq/registry_update_test.go` (nuovo)

Testare attraverso il percorso `[registry].url` con `httptest.NewTLSServer`
(il percorso GitHub hardcoda `api.github.com` e non è testabile offline).
Servono: config utente temporanea (`XDG_CONFIG_HOME` → temp dir con
`config.toml` contenente `[registry] url=.../public_key=...`), cache temp
(`XDG_CACHE_HOME`), e il client HTTP deve fidarsi del cert di test — verificare
se `download.NewClient()` consente di iniettare un transport; in alternativa
usare `httptest.NewServer` (HTTP) e... NO: l'update rifiuta http. Soluzione
pragmatica: consentire nel comando un hook non esportato per il client HTTP
(variabile `updateHTTPClient` sostituibile nel test), oppure accettare
`https://127.0.0.1` del TLS server impostando il client di test con
`ts.Client()`. Scegliere la soluzione più piccola che funzioni.

**Fixture firmate**: `go-minisign` è solo-verify, quindi le firme di test
vanno prodotte dal test stesso con `crypto/ed25519` + `golang.org/x/crypto/blake2b`
(entrambe già nel grafo delle dipendenze). Formato minisign:

- chiave pubblica (stringa base64): `"Ed"` (alg, 2 byte) || key_id (8 byte) ||
  chiave ed25519 (32 byte);
- file `.minisig`:
  ```
  untrusted comment: <testo libero>
  base64( "ED" || key_id || sig64 )
  trusted comment: <testo libero>
  base64( global_sig64 )
  ```
  dove per l'algoritmo prehashed `"ED"`: `sig64 = ed25519.Sign(sk,
  blake2b512(contenuto file))` e `global_sig64 = ed25519.Sign(sk, sig64 ||
  trusted_comment_text)`.
  **Attenzione**: confermare i dettagli esatti leggendo il sorgente di
  `github.com/jedisct1/go-minisign` (in particolare se `Verify` accetta l'alg
  legacy `"Ed"` non-prehashed, che è ancora più semplice: `sig64 =
  ed25519.Sign(sk, contenuto)`); scrivere un helper `signMinisign(t, sk,
  keyID, content) string` e un test che prima di tutto verifichi l'helper con
  `verify.CheckMinisign`.

Casi da coprire:
- happy path: update da server locale → snapshot installato, meta corretta,
  `paq registry list` vede il nuovo spec con origin `registry`;
- firma invalida → errore con "signature verification failed" (→ exit 4);
- tarball manomesso (sha256 sbagliato) → errore "integrity check";
- `url` senza `public_key` → errore;
- `url` http:// → errore;
- downgrade rifiutato senza `--force`, applicato con `--force`;
- stessa versione → "already up to date", nessun re-install;
- archivio oversize (>10 MiB) → rifiuto;
- archivio senza recipe o senza VERSION → rifiuto, cache precedente intatta.

---

## FASE 3 — Visibilità del registry nei comandi [TODO]

Tutti i punti usano `loadConfigWithMeta()` (già disponibile) e/o `Spec.Origin`.

### 3.1 `paq registry status` — `cmd/paq/registry_status.go` (nuovo)

- Registrare in `registryCmd` e aggiungere `"paq registry status"` a
  `jsonCapableCommands` in `cmd/paq/root.go`.
- Output umano: versione embedded (= `Version` del binario, var in
  `cmd/paq/version.go`), poi se snapshot presente: versione, tag, source URL,
  età fetch ("3 days ago"), numero recipe, elenco (ordinato) degli spec
  embedded sovrascritti dallo snapshot e degli spec sovrascritti da
  `[specs.*]`; se assente: "external registry: not installed (run `paq
  registry update`)". Se la cache è corrotta (`registry.Open()` errore):
  warning con l'errore.
- Output JSON: struct speculare (`{"embedded_version":..., "external":
  {"version":..., "tag":..., "fetched_at":..., "source_url":...,
  "spec_count":...} | null, "overridden_by_external": [...],
  "overridden_by_user": [...]}`).
- Per calcolare gli override: caricare embedded specs e confrontare con
  `cfg.Specs[name].Origin`.

### 3.2 Colonna SOURCE — `internal/ui/table.go` + `cmd/paq/registry_list.go`

- `ui.RegistryEntry`: aggiungere campo `Source string \`json:"source"\``.
- `PrintAvailableTable`: aggiungere colonna `SOURCE` (stile dim, come BACKEND)
  in entrambi i rami (color e no-color).
- `listDefinitions` (`registry_list.go`): popolare `Source: spec.Origin`.

### 3.3 `paq registry show` — `cmd/paq/registry_show.go` + `internal/ui/table.go`

- `PrintSpecDetail` (table.go:374): prima riga "Source: embedded" /
  "registry (v0.4.0)" / "user". Passare l'informazione dal comando (che usa
  `loadConfigWithMeta` per la versione dello snapshot). Aggiornare l'output
  JSON del comando di conseguenza (campo `source`).

### 3.4 `paq info` — `cmd/paq/info.go`

- Dove viene mostrata la definizione usata dall'app, aggiungere l'origine:
  es. `definition: ripgrep (registry v0.4.0)` oppure `(embedded)` / `(user)`.
  Campo `source` nell'output JSON.

### 3.5 `paq version` — `cmd/paq/version.go`

- In `versionInfo()` aggiungere una riga: `registry:  v0.4.0 (external)` se
  `registry.Open()` ritorna meta valida, altrimenti `registry:  embedded`.
  Nota: `versionInfo()` è chiamata anche da `init()` per `rootCmd.Version` —
  verificare che leggere la cache lì non abbia effetti collaterali indesiderati
  (niente rete, solo un file locale: accettabile; in caso contrario limitare
  la riga al solo comando `paq version`).

### 3.6 `paq config show` — `cmd/paq/config_show.go`

- Mostrare la tabella `[registry]` (url, public_key troncata) se configurata,
  e lo stato dello snapshot (versione o "not installed"). Anche in JSON.

### 3.7 `paq doctor` — `cmd/paq/doctor.go`

- Riga `Registry`: `embedded only (N specs)` oppure `external v0.4.0, fetched
  3 days ago, N specs (<path cache>)`. `WarnField` (o equivalente esistente)
  se `registry.Open()` fallisce, con hint `run 'paq registry update'`.

---

## FASE 4 — Pipeline di release, e2e, docs [TODO]

### 4.1 `.github/workflows/release.yml`

Dopo lo step esistente che genera i checksum dei binari, aggiungere:

```yaml
- name: Package registry
  run: |
    cp VERSION embedded/registry/VERSION
    tar -C embedded -czf artifacts/registry.tar.gz registry
    rm embedded/registry/VERSION
    cd artifacts && sha256sum registry.tar.gz > registry.tar.gz.sha256

- name: Sign registry checksums
  env:
    MINISIGN_SECRET_KEY: ${{ secrets.MINISIGN_SECRET_KEY }}
  run: |
    sudo apt-get update -qq && sudo apt-get install -y -qq minisign
    printf '%s\n' "$MINISIGN_SECRET_KEY" > /tmp/minisign.key
    minisign -S -s /tmp/minisign.key -m artifacts/registry.tar.gz.sha256
    rm /tmp/minisign.key
```

e aggiungere `artifacts/registry.tar.gz*` alla lista `files:` dello step di
release (adattare i path allo layout reale del workflow: verificare dove
vengono creati gli artifact e il nome dello step di upload).
`tar -C embedded ... registry` produce esattamente il layout `registry/*.toml`
atteso dalla cache. `.sdlc/release` non cambia.

Nota primo rilascio: `paq registry update` eseguito contro l'ultima release
*precedente* a questa feature fallisce con "asset not found" (messaggio già
descrittivo di `GitHubBackend.Resolve`) — accettabile.

### 4.2 e2e — `e2e/e2e_test.go`

- Test di degradazione offline: con `XDG_CACHE_HOME` puntato a una dir temp
  contenente una cache corrotta (`paq/registry/` senza `meta.json`),
  `paq registry list`, la completion dinamica e `paq doctor` devono
  funzionare (exit 0) con il warning solo su stderr.

### 4.3 Docs

- `README.md`: sezione sul registry esterno (`paq registry update`, tabella
  `[registry]` con url/public_key, dove sta la cache, modello di fiducia).
- `docs/` (sito Hugo): aggiornare la reference della configurazione.

---

## Sicurezza (riferimento per il reviewer)

- La firma minisign copre il file sha256, che pinna il tarball: stessa catena
  già implementata da `verify.Run` (firma verificata PRIMA di fidarsi del
  checksum). Fallimento = exit 4, mai fallback a contenuto non verificato.
- Un registry malevolo controlla `repo`/`source`/`chmod` dei recipe ⇒ può far
  installare binari arbitrari: la firma è LA mitigazione.
- Estrazione solo post-firma; anti path-traversal già in `internal/archive`;
  cap 10 MiB; staging nella cache dir (mai /tmp condiviso).
- URL custom: solo https, richiede `public_key` propria (sostituzione
  esplicita del trust anchor); la verifica non è mai disattivabile.
- Anti-downgrade best-effort via `version.Compare` sulla meta cached.

## Verifica end-to-end finale

1. `./.sdlc/build && ./.sdlc/test`.
2. Simulare la pipeline: creare il tarball come nella CI, firmarlo con un
   keypair di test, servirlo via HTTPS locale, configurare `[registry]` →
   `paq registry update` → `paq registry status` mostra lo snapshot;
   `paq registry show <tool>` riflette un recipe modificato; `paq registry
   list` mostra SOURCE = registry.
3. Corrompere `meta.json` → `paq search` funziona con warn su stderr;
   `paq doctor` segnala la cache corrotta.
4. `./.sdlc/e2e`.
