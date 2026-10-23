# Plán: vydávání nylzod a self-host Compose (AK15)

Datum: 23. 10. 2026. Větev: `claude/release-pipeline` z `main` po sloučení PR 6 (`899b604`). Zdroj pravdy: `docs/designs/nylzo-architecture-research.md` (sekce „Stack a verze“, „Vlastní provoz (firmy)“, „Distribution Plan“, Next Steps 7, AK15 a AK17). Stav: vlastník schválil D42 až D45 podle doporučení 23. 10. 2026. Kontrola celého kódu téhož dne přidala D51 (Compose) a D52 (vydání); plán popisuje stav po těchto opravách.

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
  - hook `before` sestaví klienta přes `pnpm`. Spouští ho lokální build a CI, job vydání ho vynechá (`--skip=before`) a klienta dostane hotového z jobu `web` (D52);
  - build `nylzod` s `CGO_ENABLED=0`, `-trimpath`, časem souborů a buildu z commitu a verzí z tagu přes `-X main.version`. `kos` bere `main`, `env`, `flags` a `ldflags` z buildu přes YAML kotvy, takže image nese stejnou binárku jako archiv;
  - archivy pro linux/amd64 a linux/arm64, `checksums.txt` a SBOM přes syft;
  - `kos` pro image `ghcr.io/misaon/nylzo` (amd64 a arm64) na základu z D45 s SBOM;
  - podpis `checksums.txt` a image přes cosign keyless;
  - patička vydání pro uživatele: `cosign verify-blob` pro `checksums.txt` s jeho `.sigstore.json`, `sha256sum -c`, `gh attestation verify` s `--signer-workflow`, `--source-ref` a `--deny-self-hosted-runners` a `cosign verify` image. Bez `--signer-workflow` by `gh` přijal provenance z kteréhokoli workflow v repozitáři.
- `.github/workflows/release.yml` spouští tag `v*`, akce má připnuté na SHA, nepoužívá cache a hodnoty z kontextu předává skriptům jen přes proměnné prostředí. Má tři joby:
  - `guard` (`contents: read`): tag je anotovaný, GitHub ověřil jeho podpis, tagger má e-mail z proměnné repozitáře `RELEASE_TAGGER_EMAIL` (bez proměnné job selže), `refs/tags/$TAG^{commit}` je `GITHUB_SHA` a ten je předkem `origin/main`. Prostředí `release` musí vyžadovat schválení;
  - `web` (`contents: read`, bez prostředí a tajemství): spustí hooky `before` přečtené z `.goreleaser.yaml` přes `yq`, takže klienta staví stejné příkazy jako CI a lokální build. `internal/server/web/dist` nahraje jako artefakt s manifestem SHA-256 a SHA-256 manifestu předá jako výstup jobu;
  - `release` (prostředí `release`, `contents: write`, `packages: write`, `id-token: write`, `attestations: write`): znovu ověří, že tag ukazuje na `GITHUB_SHA`, protože schválení může trvat hodiny. Stáhne artefakt s kontrolou digestu, porovná SHA-256 manifestu s výstupem jobu `web` a znovu spočítaný manifest se staženým, takže neprojde změněný, chybějící ani přidaný soubor. Referenci základu image přečte z `.goreleaser.yaml` (`yq`, právě jeden záznam `kos`, přesný tvar s digestem) a ověří její podpis cosignem. Pak spustí `goreleaser release --clean --skip=before` s `GORELEASER_CURRENT_TAG` nastaveným na ověřený tag, takže v jobu s právy zápisu a s OIDC tokenem neběží žádný kód z npm a jiný tag na stejném commitu, třeba RC nebo nepodepsaný, vydání nepřejmenuje. Vydání tím nese právě ověřený tag. Kontrola, že `dist/metadata.json` nese tento tag, běží až po zveřejnění, takže rozdíl jen odhalí a zastaví přidání provenance; zabraňuje mu připnutí tagu. Nakonec job přidá provenance pro archivy, checksums a image.
- CI job „Reproducible build“: dva joby na různých runnerech (x64 a arm64) sestaví klienta a binárky pro obě platformy. Třetí job porovná jejich SHA-256 a zároveň snapshot konfigurace GoReleaseru (`goreleaser check`). Node se tu instaluje s `package-manager-cache: false`, stejně jako v jobu `web`.
- `deploy/compose/` (D51):
  - `compose.yaml` s `nylzod` a Caddy, obojí připnuté na digest;
  - pepper jako Docker secret ze souboru;
  - data ve volume;
  - síť s IPv4 i IPv6 (ULA `fd1f:9791:e50b::/64`), aby Docker neposílal klienty IPv6 přes userland proxy z jedné adresy. Caddy má pevné adresy `172.30.80.2` a `fd1f:9791:e50b::2` a jen ty jsou v `NYLZO_TRUSTED_PROXIES`, brána sítě důvěru nemá. Ostatní kontejnery dostávají adresy z `ip_range` v horní polovině podsítí;
  - `NYLZO_GLOBAL_DAILY_CREATES` ve výchozím stavu 2000, takže globální strop a PoW od jeho poloviny platí bez další konfigurace;
  - `Caddyfile` s TLS a HTTP/3, globálním `log default`, který vyřazuje loggery `http.log`, `http.handlers` a `http.stdlib`, a s `keepalive 25s` ke `nylzod`, jehož `IdleTimeout` je 30 s. Hlavičky nechává na serveru;
  - README s postupem, ověřením digestu image (`gh attestation verify` s `--signer-workflow`, `cosign verify`) a s tím, co příklad nastavuje.
- Server: `NYLZO_PEPPER_FILE` a `NYLZO_PEPPER_PREVIOUS_FILE` (D44), testy a dokumentace. Spec §5.1 a README to zmíní.

## Bezpečnost

- Provenance nekryje změnu zdroje. Tag musí být podepsaný e-mailem z `RELEASE_TAGGER_EMAIL`, ukazovat na sestavovaný commit v `main` a vydání schvaluje vlastník (D43, D52). Postup zveřejnění vlastního hashe od maintainera zůstává v návrhu.
- Workflow vydání nemá `pull_request_target`, nesdílí cache s PR a nezapisuje do ní. Práva zápisu a OIDC token má jen job `release`, ostatní joby jen `contents: read`.
- Kód z npm (pnpm, Vite, Rollup, Svelte, Tailwind) běží jen v jobu `web`, který smí repozitář jen číst a nemá token pro registr ani podpis. Výstup klienta ovlivnit může: build, který se ve vydání chová jinak než jinde, odhalí nezávislé sestavení stejného tagu (AK15, README) a závislosti z lockfile hlídá politika pnpm v CI. Manifest chrání přenos mezi joby.
- Image běží bez shellu jako nonroot a s jen pro čtení kořenovým systémem souborů v Compose. Zapisuje jen do volume s daty.
- Pepper v Compose je soubor s právy 0400 předaný jako secret. Návod ho generuje přes `openssl rand -hex 32`.
- Caddy v Compose nezapíše adresu klienta, id zprávy ani hlavičky `X-Nylzo-*` ani při 502 nebo přerušeném stahování (spec §10).

## Kroky

1. Plán a rozhodnutí (tento dokument).
2. Návrh, spec §5.1 a README: D42 až D45, reprodukovatelnost, postup vydání a ověření.
3. Server: `*_FILE` pro pepper s testy.
4. Připnutí Go a Node, `.goreleaser.yaml`, `release.yml`, job reprodukovatelnosti v CI.
5. Compose s Caddy a lokální ověření AK17 (5 MB soubor) proti image sestavenému z větve.
6. Opravy z kontroly celého kódu: D51 v Compose, D52 ve workflow vydání.
7. PR proti `main`; první vydání spustí vlastník po založení prostředí `release` a proměnné `RELEASE_TAGGER_EMAIL`.

## Testy

- Go: pepper ze souboru, chyby u souboru, který chybí, je prázdný nebo má špatný obsah, a odmítnutí proměnné i souboru zároveň.
- CI: reprodukovatelnost na dvou architekturách runneru, `goreleaser check` a `goreleaser release --snapshot` bez publikace.
- Lokálně: Compose s image z `goreleaser release --snapshot`, e2e tok vytvoření a čtení 5 MB souboru přes Caddy s TLS.

## Ověření (23. 10. 2026)

Lokálně na macOS (Docker Desktop, Docker Engine 29.8.2, Compose 5.6.0), GoReleaser 2.18.3, cosign 3.1.4, Caddy 2.11.7 připnutý na digest. Image vznikl z kopie pracovního stromu větve, včetně rozpracovaných změn serveru a klienta z téhož dne. Skripty a klient pro kontrolu Compose zůstaly mimo repozitář.

- Kontroly: `actionlint` bez nálezů, `zizmor` 1.31.1 offline bez nálezů (původně hlásil chybějící `package-manager-cache: false` v CI). Persona `auditor` chce u práv jobu komentáře, které pravidla repozitáře v YAML nepřipouštějí. `goreleaser check` projde a `caddy validate` nad `Caddyfile` také.
- Kontrolní job (`guard`): jeho skript z workflow běžel proti místnímu repozitáři s náhradou `gh`. Projde jen podepsaný tag na `main` u sestavovaného commitu. Selže přesně na své kontrole, když tag mezitím ukazuje jinam, commit není v `main`, tag je lehký, proměnná chybí, tagger je jiný e-mail nebo GitHub podpis neověřil. Opakovaná kontrola v jobu `release` selže, když se tag po kontrole přesunul.
- Klient a manifest: kroky jobů `web` a `release` běžely přímo z workflow (vytažené přes `yq`). Neupravený artefakt projde. Změněný, přidaný i chybějící soubor, manifest přepočítaný po změně, cizí SHA-256 z jobu `web` i už existující `dist` v pracovním adresáři kontrolu zastaví.
- Stejné bajty po obou cestách: klient z hooků přečtených `yq` je shodný s klientem z hooků GoReleaseru (CI a lokální build). Binárky z `goreleaser release --skip=before` jsou shodné s binárkami z `goreleaser build`. Binárka v image (`/ko-app/nylzod`, linux/amd64) je shodná s binárkou z archivu, takže kotvy drží image a archiv u stejného buildu. Log vydání ukazuje vynechané hooky `before`.
- Základ image: reference přečtená z `.goreleaser.yaml` projde `cosign verify` s identitou distroless (digest `sha256:d620…3e88`), s cizí identitou selže.
- Ověření pro uživatele: `gh attestation verify` přijme `--signer-workflow`, `--source-ref` a `--deny-self-hosted-runners` (bez vydání vrátí 404, protože attestace ještě neexistuje), `cosign verify-blob` přijme `--bundle` s identitou workflow. `docker buildx imagetools inspect --format '{{.Manifest.Digest}}'` vrací digest indexu.
- Compose (AK12, AK17) s image `goreleaser.ko.local:0.0.1-snapshot` a dodaným `Caddyfile`, porty jen na `127.0.0.1` a `NYLZO_DOMAIN=localhost`, takže Caddy vydal certifikát vlastní CA a klient jí důvěřoval:
  - 5 MB tok přes Caddy (HTTP/2, TLS 1.3): vytvoření 201 (objekt 5 308 447 B se souborem 5 242 880 B), claim 200 se stejnými bajty, dešifrování dalo soubor se shodným SHA-256, blob 200 se stejnými bajty, ack 204 a metadata pak `opened`.
  - 21 vytvoření z jednoho klienta, každé s jinou podvrženou `X-Forwarded-For`, `X-Real-IP` a `Forwarded`: 1. až 20. dostalo 201, 21. 429 `rate_limited` s `retry_after` 372 s, tedy hodinovým limitem vytváření. Klient uvnitř sítě pak přes Caddy po IPv4 i IPv6 dostal 201, takže server počítá skutečného klienta z hlavičky od Caddy. Kontejner, který šel přímo na `nylzod` s `X-Forwarded-For` nastavenou na vyčerpanou adresu brány, dostal 201, takže jiný odesílatel než Caddy důvěru nemá.
  - Log Caddy: se zastaveným `nylzod` dostaly tři claimy s hlavičkami `Authorization` a `X-Nylzo-*`, tři `/m/{id}` a tři `/api/v1/instance` odpověď 502 a Caddy nezapsal jediný řádek. Totéž u stahování 5 MB, které klient přerušil po HTTP/1.1 i HTTP/2. S původním `Caddyfile` zapsal Caddy u 502 adresu klienta, cestu s id zprávy i s dotazem, hlavičky `X-Nylzo-*` a `User-Agent` (logger `http.log.error`) a u přerušeného stahování adresu klienta, cestu s id zprávy a hlavičky požadavku (logger `http.handlers.reverse_proxy`). Běžící Caddy má podle admin API `idle_timeout` 25 s.
  - Docker Desktop posílá všechny klienty z adresy brány sítě (`172.30.80.128` v logu kontrolního běhu), proto README doporučuje pro provoz Linux s Docker Engine.

Neověřeno lokálně: workflow vydání běží jen na skutečném tagu (API GitHubu pro podpis tagu, artefakty mezi joby, OIDC, push na ghcr.io), porovnání runnerů x64 a arm64 běží jen v CI. Zachování adres klientů IPv6 přes `ip6tables` vyžaduje Linux s Docker Engine, Docker Desktop ho neukáže.
