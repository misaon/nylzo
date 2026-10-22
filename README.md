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
| `NYLZO_PEPPER_PREVIOUS` | prázdná | předchozí pepper při rotaci; starší řádky se ověřují jím, nové se zapisují aktuálním. Po 7 dnech už žádná živá zpráva starý pepper nepotřebuje. Náhrobky ho drží ještě 7 dní a vyexpirovaná zpráva se stane náhrobkem až při dalším požadavku nebo úklidu, takže pro úplné stavy ho nechte 14 dní a jeden interval úklidu |
| `NYLZO_LISTEN`, `PORT` | `:8080` | adresa naslouchání |
| `NYLZO_DATA_DIR` | `data` | adresář pro SQLite a bloby; souborový systém musí podporovat hard linky (server to ověří při startu), takže FAT, exFAT a FUSE připojení objektových úložišť nefungují |
| `NYLZO_TRUSTED_PROXIES` | prázdná | CIDR proxy, od kterých se bere `X-Forwarded-For` |
| `NYLZO_PROXY_MODE` | prázdná | `scaleway` čte `X-Real-IP` |
| `NYLZO_PASSPHRASE_IP_SCOPE` | `on` | `off` váže backoff passphrase jen na zprávu, ne na kyblík adresy |
| `NYLZO_SWEEP_INTERVAL` | `5m` | interval vestavěného úklidu, `0` vypne |
| `NYLZO_MAX_FILE_BYTES` | `5242880` | limit souboru; lze jen snížit, protokolové maximum vynucují obě jádra |
| `NYLZO_MAX_TTL`, `NYLZO_DEFAULT_TTL` | `168h`, `24h` | platnost zprávy; maximum je 7 dní, minimum 60 s |
| `NYLZO_MAX_OPENS` | `10` | maximum otevření |
| `NYLZO_DEFAULT_KDF` | `1` | výchozí profil passphrase hlášený klientům (1 scrypt, 2 PBKDF2); jiné hodnoty server odmítne |
| `NYLZO_RATE_CREATE_HOUR`, `NYLZO_RATE_CREATE_DAY`, `NYLZO_RATE_CREATE_BYTES_DAY` | `20`, `100`, `50000000` | limity vytváření na kyblík /32 nebo /64; na /24 nebo /48 platí pětinásobek |
| `NYLZO_RATE_COARSE_MULTIPLIER` | `5` | násobek limitů pro hrubý kyblík /24 nebo /48. Požadavek, který odmítne limit /32 nebo /64, se do limitu /24 nebo /48 nezapočítá |
| `NYLZO_RATE_READ_HOUR` | `300` | metadata, claim a blob na kyblík |
| `NYLZO_RATE_ABUSE_HOUR` | `5` | hlášení zneužití na kyblík |
| `NYLZO_RATE_SOURCE_RPS`, `NYLZO_RATE_SOURCE_BURST` | `10`, `20` | paměťový token bucket jednoho zdroje (kyblík /32 nebo /64) pro `/api/*`. Za ním následuje bucket rozsahu /24 nebo /48 s polovinou kapacity instance, takže jeden zdroj ani jeden rozsah nevezme víc než polovinu bucketu instance |
| `NYLZO_RATE_BUCKET_RPS`, `NYLZO_RATE_BUCKET_BURST` | `50`, `100` | paměťový token bucket instance pro `/api/*` |
| `NYLZO_GLOBAL_DAILY_CREATES` | `0` (vypnuto) | denní strop vytvoření; od 50 % vyžaduje PoW, při 100 % vrací 503. Místo se rezervuje atomicky až po PoW a při každém selhání se vrátí, takže souběh strop nepřekročí. Dokud uložené zprávy samy strop nezaplní, 503 radí zkusit to za 60 s. Platí to i pro místa, která drží nahrávání přerušené pádem procesu, takže rada na 60 s se může opakovat až do konce dne UTC. Jakmile strop zaplní uložené zprávy, radí počkat do dalšího dne |
| `NYLZO_POW_DIFFICULTY`, `NYLZO_POW_CONCURRENCY` | `4`, `4` | obtížnost (nejvýš 8) a souběh ověření PoW (každé ověření drží 16 MiB; strop souběhu je 16). Každou výzvu lze použít jen jednou |

Za reverzní proxy nastavte `NYLZO_TRUSTED_PROXIES` nebo `NYLZO_PROXY_MODE`. Bez nich server vidí jen adresu proxy a všichni klienti sdílejí jeden kyblík, včetně limitu na zdroj.

Hodnota `0` u proměnných `NYLZO_RATE_*_HOUR`, `NYLZO_RATE_*_DAY` a `NYLZO_RATE_COARSE_MULTIPLIER` limit vypíná, neblokuje.

Hlášení zneužití s platným tokenem čtení se zapisují do `data/abuse-reports.jsonl` pro ruční posouzení. Hlášení musí mít `Content-Type: application/json`. Server důvod hlášení dekóduje z procentového kódování, vymaže z něj fragmenty odkazů a dlouhé tokeny a zkrátí ho. Na jednu zprávu a jeden rozsah adres /24 nebo /48 uloží nejvýš 3 hlášení denně, takže odesílatel zprávy nemůže limit vyčerpat ostatním. Když by hlášení soubor zvětšilo nad 16 MiB, server na hlášení, které by uložil, odpovídá 503 a loguje chybu `abuse-file-full`; soubor je pak potřeba zpracovat a vyprázdnit.

Logy obsahují čas, metodu, vzor cesty, id zprávy (jen platné), stav, latenci a velikost odpovědi. Nikdy neobsahují surovou cestu, hlavičku `Authorization`, hlavičky `X-Nylzo-*`, těla ani adresy klientů. Interní chyby `net/http` se logují jen jako kategorie bez textu.

`nylzod sweep-once` skončí nenulovým kódem, když některý krok úklidu selže. Vestavěný úklid běží i hned po startu. Denní kontrola osiřelých blobů se zopakuje při dalším úklidu, dokud se jí nepodaří vypsat řádky i úložiště blobů. Burn a cancel si před smazáním blobu zapíšou zamýšlený stav. Expirovaná zpráva zápis nepotřebuje, protože ji ukončuje samo `expires_at`. Po pádu procesu konec dokončí další požadavek nebo úklid. Chybějící blob u neexpirované zprávy bez zapsaného stavu server hlásí jako chybu 500, zprávu nesmaže a claim kvůli němu nespotřebuje otevření.

Při upgradu zastavte staré procesy dřív, než nový `nylzod` otevře databázi; přidá do ní sloupec `ending`, který starší verze nečtou. Před návratem ke starší verzi nechte doběhnout úklid, aby žádná zpráva neměla rozpracovaný konec.

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
