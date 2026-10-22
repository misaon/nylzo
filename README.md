# Nylzo

Nylzo je open-source služba pro jednorázové sdílení citlivých zpráv a souborů do 5 MB. Obsah se šifruje v klientovi, server dostává jen ciphertext a HMAC důkazy; odkaz nese klíč jen ve fragmentu URL, který prohlížeč na server nikdy neposílá. Návrh a jeho zdůvodnění jsou v `docs/designs/nylzo-architecture-research.md`, normativní zápis protokolu v1 je v `spec/protocol.md`.

Tento repozitář obsahuje první milník: protokol v1, jádro v Go a v TypeScriptu svázané společnými vektory a server `nylzod` s úložištěm SQLite a disk. Webový klient, CLI a adaptér postgres+s3 zatím nejsou součástí.

## Rozložení repozitáře

| Cesta | Obsah |
|---|---|
| `spec/protocol.md` | normativní protokol v1 |
| `spec/vectors.json` | testovací vektory generované z Go jádra |
| `internal/core` | Go jádro: odvození klíčů, KDF profily, obálka, meta JSON, odkaz |
| `internal/pepper` | serverové klíče podle účelu z pepperu, rotace, kyblíky adres |
| `internal/store` | rozhraní úložiště, adaptér `sqlite` a `disk`, sdílená testovací sada |
| `internal/server` | HTTP server: endpointy, limity, PoW, hlavičky, úklid |
| `cmd/nylzod` | binárka serveru (`serve`, `sweep-once`, `version`) |
| `cmd/nylzo-vectors` | generátor vektorů |
| `web/core` | TypeScript jádro `@nylzo/core` (Web Crypto, scrypt z `@noble/hashes`) |

## Požadavky

- Go 1.27 (na tomto stroji přes `mise`: `mise use go@1.27`).
- Node.js 24 a pnpm 12 (pole `packageManager` ve `web/package.json` ho přes corepack stáhne samo).

## Spuštění serveru lokálně se SQLite

Server potřebuje 32bajtový pepper, tajemství mimo databázi, kterým podepisuje uložené tokeny. Vygenerujte ho a spusťte server:

```bash
export NYLZO_PEPPER="$(openssl rand -hex 32)"
go run ./cmd/nylzod
```

Server naslouchá na `:8080`, data ukládá do `./data` (soubor `nylzo.db` a adresář `blobs`). Pepper si uložte; bez něj server žádný existující token neověří a živé zprávy zaniknou.

Kontrola, že běží:

```bash
curl -s http://localhost:8080/api/v1/instance
```

Stránky `/` a `/m/{id}` zatím servírují jen zástupné HTML; webový klient vzniká zvlášť.

Úklid běží uvnitř serveru každých 5 minut. Pro nasazení s externím plánovačem nastavte `NYLZO_SWEEP_INTERVAL=0` a spouštějte:

```bash
go run ./cmd/nylzod sweep-once
```

Příkaz `sweep-once` pepper nepotřebuje.

### Proměnné prostředí

| Proměnná | Výchozí | Význam |
|---|---|---|
| `NYLZO_PEPPER` | povinná pro `serve` | 32 bajtů v hex nebo base64url |
| `NYLZO_PEPPER_PREVIOUS` | prázdná | předchozí pepper při rotaci; starší řádky se ověřují jím, nové se zapisují aktuálním |
| `NYLZO_LISTEN`, `PORT` | `:8080` | adresa naslouchání |
| `NYLZO_DATA_DIR` | `data` | adresář pro SQLite a bloby |
| `NYLZO_TRUSTED_PROXIES` | prázdná | CIDR proxy, od kterých se bere `X-Forwarded-For` |
| `NYLZO_PROXY_MODE` | prázdná | `scaleway` čte `X-Real-IP` |
| `NYLZO_PASSPHRASE_IP_SCOPE` | `on` | `off` váže backoff passphrase jen na zprávu, ne na kyblík adresy |
| `NYLZO_SWEEP_INTERVAL` | `5m` | interval vestavěného úklidu, `0` vypne |
| `NYLZO_MAX_FILE_BYTES` | `5242880` | limit souboru; lze jen snížit, protokolové maximum vynucují obě jádra |
| `NYLZO_MAX_TTL`, `NYLZO_DEFAULT_TTL` | `168h`, `24h` | platnost zprávy |
| `NYLZO_MAX_OPENS` | `10` | maximum otevření |
| `NYLZO_DEFAULT_KDF` | `1` | výchozí profil passphrase hlášený klientům (1 scrypt, 2 PBKDF2) |
| `NYLZO_RATE_CREATE_HOUR`, `NYLZO_RATE_CREATE_DAY`, `NYLZO_RATE_CREATE_BYTES_DAY` | `20`, `100`, `50000000` | limity vytváření na kyblík /32 nebo /64; na /24 nebo /48 platí pětinásobek |
| `NYLZO_RATE_COARSE_MULTIPLIER` | `5` | násobek limitů pro hrubý kyblík /24 nebo /48 |
| `NYLZO_RATE_READ_HOUR` | `300` | metadata, claim a blob na kyblík |
| `NYLZO_RATE_ABUSE_HOUR` | `5` | hlášení zneužití na kyblík |
| `NYLZO_RATE_BUCKET_RPS`, `NYLZO_RATE_BUCKET_BURST` | `50`, `100` | paměťový token bucket instance pro `/api/*` |
| `NYLZO_GLOBAL_DAILY_CREATES` | `0` (vypnuto) | denní strop vytvoření; od 50 % vyžaduje PoW, nad 100 % vrací 503 |
| `NYLZO_POW_DIFFICULTY`, `NYLZO_POW_CONCURRENCY` | `4`, `4` | obtížnost a souběh ověření PoW (každé ověření drží 16 MiB; strop souběhu je 16) |

Hlášení zneužití s platným tokenem čtení se zapisují do `data/abuse-reports.jsonl` pro ruční posouzení.

Logy obsahují čas, metodu, cestu, stav, latenci a velikost odpovědi; nikdy hlavičku `Authorization`, hlavičky `X-Nylzo-*`, těla ani adresy klientů.

## Generování vektorů

Vektory v `spec/vectors.json` vznikají deterministicky z Go jádra. Po změně jádra je vygenerujte znovu a commitněte:

```bash
go run ./cmd/nylzo-vectors -out spec/vectors.json
```

Test `internal/core/vectors` selže, když commitnutý soubor neodpovídá generátoru, a testy `web/core` selžou, když TypeScript jádro vektory neprojde. CI obojí kříží: změna jednoho jádra bez druhého CI shodí.

## Testy

Go:

```bash
go test -race ./...
```

Čtvrtý vektor scrypt z RFC 7914 potřebuje 1 GiB paměti; `go test -short` přeskočí jeho přepočet v `internal/core`, test generátoru v `internal/core/vectors` ho počítá vždy.

TypeScript:

```bash
cd web && pnpm install --frozen-lockfile && pnpm -r typecheck && pnpm -r test
```

Těžký vektor v TypeScriptu běží jen s `NYLZO_HEAVY_TESTS=1` (CI ho zapíná).

Fuzz cíle (obálka, meta JSON, odkaz, hlavička vytvoření):

```bash
go test -run='^$' -fuzz=FuzzOpen -fuzztime=30s ./internal/core
```

## Licence

MIT, viz `LICENSE`.
