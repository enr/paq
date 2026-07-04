# Piano — Attivazione della firma minisign del registry

Stato: la verifica della firma del registry è **temporaneamente opzionale**.
Questo documento descrive, passo per passo e senza ambiguità, come attivarla
in modo definitivo quando si decide di farlo. Ogni passo indica **dove** va
eseguito: macchina locale, interfaccia web di GitHub, o repository (modifica
di codice da committare).

---

## 1. Stato attuale (cosa è disabilitato e dove)

| Componente | Comportamento attuale | File |
|---|---|---|
| Binario, sorgente default | Se la chiave pubblica embedded è vuota, `paq registry update` procede con la **sola verifica sha256** e stampa il warning `this build has no registry signing key: signature not verified (checksum only)` | `cmd/paq/registry_update.go` (`runRegistryUpdate`, `resolveRegistrySource`) |
| Binario, sorgente custom | **Invariato**: `[registry].url` richiede sempre `https` + `public_key`, e la firma è verificata sempre | `cmd/paq/registry_update.go` |
| Release workflow, build | La variable `MINISIGN_PUBLIC_KEY` è passata al cross-build ma **può mancare**: in quel caso i binari escono senza trust anchor | `.github/workflows/release.yml`, step "Cross-build all targets" |
| Release workflow, firma | Se la secret `MINISIGN_SECRET_KEY` manca, la firma viene **saltata** (release checksum-only). Stato vietato: public key impostata senza secret → la release fallisce | `.github/workflows/release.yml`, step "Sign registry checksums" |

Cosa invece è **già pronto e non va toccato** al momento dell'attivazione:

- l'iniezione della chiave nei binari via `-ldflags -X` leggendo la env
  `MINISIGN_PUBLIC_KEY` (`.sdlc/build`, `.sdlc/cross`);
- la verifica di coerenza della coppia in CI: dopo la firma, il workflow
  verifica il `.minisig` con la chiave pubblica che verrà distribuita
  (`minisign -Vm ... -P`), quindi una coppia disallineata fa fallire la
  release invece di produrre binari rotti;
- il codice di verifica vero e proprio: quando la chiave embedded è presente,
  firma e checksum sono già verificati in quest'ordine (nessuna modifica
  necessaria).

L'attivazione consiste quindi in: creare le chiavi, configurarle su GitHub,
e ripristinare i due punti di enforcement (codice + workflow) descritti in
Fase D ed E.

---

## Fase A — Generare la coppia di chiavi (macchina locale)

Prerequisito: minisign installato.

```bash
# macOS
brew install minisign
# Debian/Ubuntu
sudo apt install minisign
# Windows
scoop install minisign
```

**A.1** — Verificare se una coppia esiste già. Su GitHub →
`Settings → Secrets and variables → Actions` → tab **Secrets**: se
`MINISIGN_SECRET_KEY` esiste e le release recenti pubblicano l'asset
`registry.tar.gz.sha256.minisig`, la chiave privata funziona.

- Se hai ancora il file `minisign.key` in locale, recupera la pubblica:

  ```bash
  minisign -R -s ~/.minisign/minisign.key -p /tmp/minisign.pub
  ```

  e salta alla Fase B.
- Se la secret esiste ma il file della privata è perso: le secret di GitHub
  non sono rileggibili, quindi la pubblica non è ricavabile. Genera una
  coppia nuova (A.2) e sovrascrivi anche la secret in Fase B.

**A.2** — Generare una coppia nuova, **fuori dal clone del repository**:

```bash
mkdir -p ~/paq-keys && cd ~/paq-keys
minisign -G -W -s minisign.key -p minisign.pub
```

Il flag `-W` (nessuna password) è **obbligatorio**: in GitHub Actions
`minisign -S` gira senza terminale e una chiave protetta da password
bloccherebbe la release.

**A.3** — Identificare le due parti. `minisign.pub` ha due righe:

```
untrusted comment: minisign public key ABC123...
RWQ...56 caratteri base64...
```

- **Variable GitHub** ← solo la **seconda riga** (56 caratteri). Con la riga
  di commento inclusa, ogni `paq registry update` fallirebbe con
  `Invalid encoded public key`.
- **Secret GitHub** ← il file `minisign.key` **intero** (entrambe le righe).

**A.4** — Test di coerenza della coppia, prima di toccare GitHub:

```bash
echo test > /tmp/f
minisign -S -s minisign.key -m /tmp/f
minisign -Vm /tmp/f -P "RWQ...la-seconda-riga..."
```

Atteso: `Signature and comment signature verified`.

**A.5** — Custodire `minisign.key` in un password manager e rimuovere i file
dalla directory di lavoro. Mai committare la privata. Se viene persa, i
binari già rilasciati non potranno più aggiornare il registry finché gli
utenti non fanno `paq self-update` verso una release firmata con la coppia
nuova (vedi Fase G).

---

## Fase B — Configurare GitHub (browser)

Percorso: `github.com/enr/paq` → **Settings → Secrets and variables →
Actions**.

**B.1** — Tab **Secrets** → `New repository secret` (o `Update`):

- Name: `MINISIGN_SECRET_KEY`
- Value: contenuto integrale di `minisign.key`

**B.2** — Tab **Variables** → `New repository variable`:

- Name: `MINISIGN_PUBLIC_KEY`
- Value: la sola riga base64 di `minisign.pub`, senza spazi o a-capo extra

Attenzione al tab giusto: la pubblica va tra le **Variables** (il workflow la
legge da `vars.`); messa tra le Secrets non verrebbe vista.

**B.3** — Vincolo di coppia: secret e variable vanno cambiate **sempre
insieme**. Ogni firma contiene un key id e i binari rifiutano firme di una
chiave diversa da quella embedded (`Incompatible key identifiers`). Il check
in CI (già presente) intercetta il disallineamento alla release.

---

## Fase C — Release di transizione (nessuna modifica di codice)

Con secret + variable impostate e **senza** ancora ripristinare
l'enforcement, fare una release normale (tag `v*`). Da questa release:

- i binari escono **con** la chiave embedded e verificano la firma;
- il registry è firmato e la coppia è validata in CI.

Verifica post-release (macchina locale): scaricare il binario appena
rilasciato ed eseguire `paq registry update`. Atteso: `Verifying
signature...` e completamento senza warning. Se fallisce con `Incompatible
key identifiers`, tornare ad A.4/B.3.

Questa fase serve a validare l'intera catena mentre il fallback
checksum-only esiste ancora: se qualcosa è storto, nessun utente resta
bloccato.

---

## Fase D — Ripristinare l'enforcement nel codice (repository)

Da fare **dopo** che la Fase C ha prodotto almeno una release verificata.

**D.1** — `cmd/paq/registry_update.go`, funzione `resolveRegistrySource`:
ripristinare il fail-closed per la sorgente default. Subito prima di
`ui.Step("Resolving latest registry release...")` reintrodurre:

```go
if registry.DefaultPublicKey == "" {
    return registrySource{}, fmt.Errorf("this build has no registry signing key; configure a custom source in [registry] (url + public_key)")
}
```

e aggiornare il commento della funzione rimuovendo la frase sul fallback
checksum-only.

**D.2** — `cmd/paq/registry_update.go`, funzione `runRegistryUpdate`:
rimuovere il ramo `else` del fallback (warning + "Verifying checksum...") e
tornare al download incondizionato della firma:

```go
sigPath, err := download.ToTemp(ctx, client, src.sigURL, nil)
if err != nil {
    return fmt.Errorf("download signature: %w", err)
}
defer os.Remove(sigPath)

ui.Step("Verifying signature...")
```

(il `verify.Run` sottostante resta identico). Rimuovere anche il commento
"Signature verification is temporarily optional".

**D.3** — `internal/registry/trust.go`: aggiornare il commento di
`DefaultPublicKey` ripristinando la semantica "an empty value means the
update from the default source must refuse to run".

**D.4** — Test: in `cmd/paq/registry_update_test.go` aggiungere un test che,
con `registry.DefaultPublicKey = ""` (è una var: salvarla e ripristinarla con
`t.Cleanup`) e **nessuna** sezione `[registry]` nel manifest, verifichi che
`runRegistryUpdate` fallisca con un errore contenente `no registry signing
key` senza effettuare alcuna richiesta di rete (usare un
`registryUpdateClient` che fa fallire il test se invocato).

**D.5** — Eseguire `go test ./...` e `go vet ./...`; committare con messaggio
tipo `Enforce registry signature verification for the default source`.

---

## Fase E — Ripristinare il guard nel workflow (repository)

`.github/workflows/release.yml`, step "Cross-build all targets": ripristinare
il fail-fast quando la variable manca, sostituendo `run: ./.sdlc/cross` con:

```yaml
        run: |
          if [ -z "${MINISIGN_PUBLIC_KEY}" ]; then
            echo "MINISIGN_PUBLIC_KEY repository variable is not set:" >&2
            echo "released binaries would have no trust anchor and 'paq registry update' would refuse to run." >&2
            exit 1
          fi
          ./.sdlc/cross
```

e aggiornare il commento dello step (rimuovere "Optional for now"). Nello
step "Sign registry checksums" rimuovere il ramo che salta la firma quando
la secret manca: con l'enforcement attivo una release non firmata non è più
uno stato lecito. Il blocco diventa:

```yaml
        run: |
          sudo apt-get update -qq && sudo apt-get install -y -qq minisign
          printf '%s\n' "$MINISIGN_SECRET_KEY" > /tmp/minisign.key
          minisign -S -s /tmp/minisign.key -m artifacts/registry.tar.gz.sha256
          rm /tmp/minisign.key
          minisign -Vm artifacts/registry.tar.gz.sha256 -P "${MINISIGN_PUBLIC_KEY}"
```

(la verifica finale della coppia resta, ora incondizionata).

Fasi D ed E vanno mergiate **nella stessa release**: sono i due lati dello
stesso contratto.

---

## Fase F — Release definitiva e verifica (macchina locale + GitHub)

1. Release normale (tag `v*`). Il workflow ora fallisce se manca una delle
   due chiavi o se la coppia non combacia.
2. Post-release: `paq self-update` da un binario precedente, poi
   `paq registry update` → deve verificare la firma.
3. Nota per le release notes: i binari da questa versione in poi
   **richiedono** release firmate; i binari delle versioni della finestra
   "opzionale" continuano a funzionare (verificano la firma se hanno la
   chiave, altrimenti checksum-only).

---

## Fase G — Rotazione della chiave (procedura futura, per riferimento)

La rotazione **rompe** `paq registry update` per tutti i binari già
distribuiti (key id diverso), finché l'utente non fa `paq self-update`.
Procedura minima:

1. generare la coppia nuova (Fase A);
2. aggiornare **insieme** secret e variable (Fase B);
3. rilasciare: i binari nuovi hanno la chiave nuova;
4. comunicare nelle release notes che i binari precedenti devono fare
   `paq self-update` prima di `paq registry update`.

Vincolo da ricordare: il self-update oggi verifica solo `SHA256SUMS` (non
firmato) ed è proprio questo a rendere possibile la via di fuga. Se si
implementa L6 di `plan/code-review-fixes.md` (firma del `SHA256SUMS`), la
rotazione va progettata esplicitamente (doppia firma di transizione o lista
di chiavi valide nel binario) **prima** di ruotare.

---

## Riepilogo esecutivo

| Fase | Dove | Contenuto | Gate |
|---|---|---|---|
| A | locale | genera/recupera coppia, test `minisign -V` | firma di prova verificata |
| B | GitHub web | secret `MINISIGN_SECRET_KEY` + variable `MINISIGN_PUBLIC_KEY` | entrambe presenti |
| C | locale | release di transizione, `paq registry update` verifica la firma | catena validata end-to-end |
| D | repository | fail-closed in `registry_update.go` + test | suite verde |
| E | repository | guard nel workflow, firma non più saltabile | stessa release di D |
| F | locale/GitHub | release definitiva + verifica post-release | update firmato |
| G | — | solo in caso di rotazione | — |
