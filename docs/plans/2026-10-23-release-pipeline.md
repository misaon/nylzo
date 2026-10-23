# Plán: vydávání nylzod a self-host Compose (AK15)

Datum: 23. 10. 2026. Větev: `claude/release-pipeline` z `main` po sloučení PR 6 (`899b604`). Zdroj pravdy: `docs/designs/nylzo-architecture-research.md` (sekce „Stack a verze“, „Vlastní provoz (firmy)“, „Distribution Plan“, Next Steps 7, AK15 a AK17). Stav: vlastník schválil D42 až D45 podle doporučení 23. 10. 2026.

## Cíl

Bod 7 Next Steps: reprodukovatelný build Go binárek i webového klienta, image na ghcr.io s podpisem, provenance a SBOM a příklad Compose s Caddy pro self-host. Hotovo znamená:

- AK15: dva nezávislé buildy dají stejné hashe binárek i klienta a `gh attestation verify` projde na binárkách i na image.
- Část AK17 pro self-host: v Compose se vytvoří a přečte 5 MB soubor.
- Workflow vydání je připravené, ale nic se nevydá, dokud vlastník nepushne podepsaný tag a vydání neschválí.

CLI (bod 6) do milníku nepatří. Konfigurace GoReleaseru je ale připravená tak, aby ho později přidal jako další build.

## Rozhodnutí vlastníka

### D42: nástroj pro build a vydání

- A. GoReleaser v2.18.3 s vestavěným ko. Jeden připnutý konfigurační soubor sestaví binárky, `checksums.txt`, SBOM a multi-arch image bez Dockerfile.
- B. `go build` a ko přímo ve workflow.
- C. Dockerfile a `docker buildx`.

Doporučení: A, jak říká návrh. ko staví image reprodukovatelně, s časem z commitu a se základem připnutým na digest. Rozhodnuto: A.

### D43: co spustí vydání

- A. Push podepsaného tagu `v*`. Job běží v GitHub prostředí `release`, které vyžaduje schválení vlastníka.
- B. Jen push tagu.
- C. Ruční `workflow_dispatch`.

Doporučení: A. Samotný tag nic nevydá a verze je svázaná s podepsaným tagem. Prostředí `release` s povinným schválením založí vlastník v nastavení repozitáře. Rozhodnuto: A.

### D44: pepper v self-host Compose

- A. Soubor přes Docker secret. Server přijme `NYLZO_PEPPER_FILE` a `NYLZO_PEPPER_PREVIOUS_FILE`.
- B. Proměnná ze souboru `.env`.

Doporučení: A. Pepper pak není v prostředí procesu, ve výpisu `docker inspect` ani v logu orchestrátoru. Server odmítne nastavení, ve kterém je zároveň proměnná i soubor. Rozhodnuto: A.

### D45: základ image

- A. `gcr.io/distroless/static-debian13:nonroot`, připnutý na digest.
- B. Chainguard static.
- C. `scratch`.

Doporučení: A, jak říká návrh. Image má CA certifikáty a tzdata a neobsahuje shell. Proces běží jako uživatel 65532. Rozhodnuto: A.

## Rozsah a struktura

- `go.mod` dostane `toolchain go1.27.3` a `web/.node-version` dostane `24.22.1`. CI i vydání používají přesně tyto verze, protože jiná verze kompilátoru nebo komprese dá jiné bajty.
- `.goreleaser.yaml`:
  - hook `before` sestaví klienta přes `pnpm`;
  - build `nylzod` s `CGO_ENABLED=0`, `-trimpath`, časem souborů a buildu z commitu a verzí z tagu přes `-X main.version`;
  - archivy pro linux/amd64 a linux/arm64, `checksums.txt` a SBOM přes syft;
  - `kos` pro image `ghcr.io/misaon/nylzo` (amd64 a arm64) na základu z D45 s SBOM;
  - podpis `checksums.txt` a image přes cosign keyless.
- `.github/workflows/release.yml`:
  - spouští ho tag `v*` a běží v prostředí `release`;
  - nejdřív ověří, že tag je anotovaný a podepsaný;
  - spustí GoReleaser a pak `actions/attest-build-provenance` pro archivy, checksums a digest image;
  - práva má minimální (`contents: write`, `packages: write`, `id-token: write`, `attestations: write`), akce připnuté na SHA a žádnou cache.
- CI job „Reproducible build“: dva joby na různých runnerech (x64 a arm64) sestaví klienta a binárky pro obě platformy. Třetí job porovná jejich SHA-256 a zároveň snapshot konfigurace GoReleaseru (`goreleaser check`).
- `deploy/compose/`:
  - `compose.yaml` s `nylzod` a Caddy, obojí připnuté na digest;
  - pepper jako Docker secret ze souboru;
  - data ve volume;
  - `NYLZO_TRUSTED_PROXIES` na síť Compose, `NYLZO_PUBLIC_URL` a doména pro Caddy z `.env`;
  - `Caddyfile` s TLS, HTTP/3 a hlavičkami ponechanými na serveru;
  - README s postupem a ověřením image (`gh attestation verify`, `cosign verify`).
- Server: `NYLZO_PEPPER_FILE` a `NYLZO_PEPPER_PREVIOUS_FILE` (D44), testy a dokumentace. Spec §5.1 a README to zmíní.

## Bezpečnost

- Provenance nekryje změnu zdroje. Tag musí být podepsaný a vydání schvaluje vlastník (D43). Postup zveřejnění vlastního hashe od maintainera zůstává v návrhu.
- Workflow vydání nemá `pull_request_target`, nesdílí cache s PR a nezapisuje do ní. Token `GITHUB_TOKEN` dostane jen práva vypsaná výše.
- Image běží bez shellu jako nonroot a s jen pro čtení kořenovým systémem souborů v Compose. Zapisuje jen do volume s daty.
- Pepper v Compose je soubor s právy 0400 předaný jako secret. Návod ho generuje přes `openssl rand -hex 32`.

## Kroky

1. Plán a rozhodnutí (tento dokument).
2. Návrh, spec §5.1 a README: D42 až D45, reprodukovatelnost, postup vydání a ověření.
3. Server: `*_FILE` pro pepper s testy.
4. Připnutí Go a Node, `.goreleaser.yaml`, `release.yml`, job reprodukovatelnosti v CI.
5. Compose s Caddy a lokální ověření AK17 (5 MB soubor) proti image sestavenému z větve.
6. PR proti `main`; první vydání spustí vlastník.

## Testy

- Go: pepper ze souboru, chyby u souboru, který chybí, je prázdný nebo má špatný obsah, a odmítnutí proměnné i souboru zároveň.
- CI: reprodukovatelnost na dvou architekturách runneru, `goreleaser check` a `goreleaser release --snapshot` bez publikace.
- Lokálně: Compose s image z `goreleaser release --snapshot`, e2e tok vytvoření a čtení 5 MB souboru přes Caddy s TLS.
