# Nylzo

Nylzo je open-source služba pro jednorázové sdílení citlivých zpráv a souborů do 5 MB. Obsah se šifruje v klientovi, server dostává jen ciphertext a HMAC důkazy; odkaz nese klíč jen ve fragmentu URL, který prohlížeč na server nikdy neposílá. Návrh a jeho zdůvodnění jsou v `docs/designs/nylzo-architecture-research.md`, normativní zápis protokolu v1 je v `spec/protocol.md`.

Tento repozitář obsahuje první milník: protokol v1, jádro v Go a v TypeScriptu svázané společnými vektory a server `nylzod` s úložištěm SQLite a disk. Webový klient, CLI a adaptér postgres+s3 zatím nejsou součástí.

## Rozložení repozitáře

| Cesta | Obsah |
|---|---|
| `spec/protocol.md` | normativní protokol v1 |
| `spec/vectors.json` | testovací vektory generované z Go jádra |
| `internal/core` | Go jádro: odvození klíčů, KDF profily, obálka, meta JSON, odkaz |
| `internal/pepper` | serverové klíče podle účelu z pepperu a rotace |
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
| `NYLZO_PEPPER_PREVIOUS` | prázdná | předchozí pepper při rotaci; starší řádky se ověřují jím, nové se zapisují aktuálním. Po 7 dnech už žádná živá zpráva starý pepper nepotřebuje. Náhrobky ho drží ještě 7 dní a vyexpirovaná zpráva se stane náhrobkem až při dalším požadavku nebo úklidu, takže pro úplné stavy ho nechte 14 dní a jeden interval úklidu. Rotace zároveň vynuluje čítače limitů, backoff hesel a limit hlášení na rozsah adres, stejně jako půlnoc UTC; denní strop 100 selhání na zprávu platí dál |
| `NYLZO_LISTEN`, `PORT` | `:8080` | adresa naslouchání |
| `NYLZO_DATA_DIR` | `data` | adresář pro SQLite a bloby; souborový systém musí podporovat hard linky a fsync adresáře (`serve` i `sweep-once` to ověří při startu), takže FAT, exFAT a FUSE připojení objektových úložišť nefungují. `sweep-once` adresář nezakládá: bez existující databáze a adresáře `blobs` skončí chybou, proto mu v plánovači dejte absolutní cestu |
| `NYLZO_TRUSTED_PROXIES` | prázdná | CIDR proxy, od kterých se bere `X-Forwarded-For` |
| `NYLZO_PROXY_MODE` | prázdná | `scaleway` čte `X-Real-IP` |
| `NYLZO_SWEEP_INTERVAL` | `5m` | interval vestavěného úklidu, `0` vypne |
| `NYLZO_MAX_FILE_BYTES` | `5242880` | limit souboru; lze jen snížit, protokolové maximum vynucují obě jádra |
| `NYLZO_MAX_TTL`, `NYLZO_DEFAULT_TTL` | `168h`, `24h` | platnost zprávy; maximum je 7 dní, minimum 60 s |
| `NYLZO_MAX_OPENS` | `10` | maximum otevření, nejvýš 100 |
| `NYLZO_DEFAULT_KDF` | `1` | výchozí profil passphrase hlášený klientům (1 scrypt, 2 PBKDF2); jiné hodnoty server odmítne |
| `NYLZO_RATE_CREATE_HOUR`, `NYLZO_RATE_CREATE_DAY`, `NYLZO_RATE_CREATE_BYTES_DAY` | `20`, `100`, `50000000` | limity vytváření na kyblík /32 nebo /64; na /24 nebo /48 platí pětinásobek |
| `NYLZO_RATE_COARSE_MULTIPLIER` | `5` | násobek limitů pro hrubý kyblík /24 nebo /48. Požadavek, který odmítne limit /32 nebo /64, se do limitu /24 nebo /48 nezapočítá |
| `NYLZO_RATE_READ_HOUR` | `300` | metadata, claim a blob na kyblík |
| `NYLZO_RATE_ABUSE_HOUR` | `5` | hlášení zneužití na kyblík |
| `NYLZO_RATE_SOURCE_RPS`, `NYLZO_RATE_SOURCE_BURST` | `10`, `20` | paměťový token bucket jednoho zdroje (kyblík /32 nebo /64) pro `/api/*`. Za ním následuje bucket rozsahu /24 nebo /48 s polovinou kapacity instance, takže jeden zdroj ani jeden rozsah nevezme víc než polovinu bucketu instance |
| `NYLZO_RATE_BUCKET_RPS`, `NYLZO_RATE_BUCKET_BURST` | `50`, `100` | paměťový token bucket instance pro `/api/*` |
| `NYLZO_GLOBAL_DAILY_CREATES` | `0` (vypnuto) | denní strop vytvoření; od 50 % vyžaduje PoW, při 100 % vrací 503. Místo se rezervuje atomicky až po PoW a při každém selhání se vrátí, takže souběh strop nepřekročí. Dokud uložené zprávy samy strop nezaplní, 503 radí zkusit to za 60 s. Platí to i pro místa, která drží nahrávání přerušené pádem procesu, takže rada na 60 s se může opakovat až do konce dne UTC. Jakmile strop zaplní uložené zprávy, radí počkat do dalšího dne |
| `NYLZO_POW_DIFFICULTY`, `NYLZO_POW_CONCURRENCY` | `4`, `4` | obtížnost (nejvýš 8) a souběh ověření PoW (každé ověření drží 16 MiB; strop souběhu je 4). Každou výzvu lze použít jen jednou |

Za reverzní proxy nastavte `NYLZO_TRUSTED_PROXIES` nebo `NYLZO_PROXY_MODE`. Bez nich server vidí jen adresu proxy a všichni klienti sdílejí jeden kyblík, včetně limitu na zdroj.

Všechny limity na kyblík adresy drží každý proces jen v paměti, s klíčem, který si vylosuje při startu. Databáze, záloha, logy ani pepper proto o adresách klientů nic neobsahují. Limity na adresu ale platí pro jednu instanci a po restartu nebo probuzení kontejneru začínají od nuly. Globální denní strop, PoW, backoff passphrase a limit hlášení na zprávu jsou v databázi a platí napříč instancemi.

Hodnota `0` u proměnných `NYLZO_RATE_*_HOUR`, `NYLZO_RATE_*_DAY` a `NYLZO_RATE_COARSE_MULTIPLIER` limit vypíná, neblokuje.

Hlášení zneužití podepsaná klíčem čtení zprávy se zapisují do `data/abuse-reports.jsonl` pro ruční posouzení. Hlášení musí mít `Content-Type: application/json`. Server důvod hlášení dekóduje z procentového kódování, řídicí znaky nahradí mezerou, neviditelné formátovací znaky (třeba přepínače směru textu) zahodí, vymaže fragmenty odkazů a dlouhé tokeny a důvod zkrátí. Na jednu zprávu uloží nejvýš 3 hlášení denně. Limit platí jen na zprávu, aby žádný řádek nespojoval zprávu s adresou, a jeden ohlašovatel ho proto může vyčerpat. Když by hlášení soubor zvětšilo nad 16 MiB, server na hlášení, které by uložil, odpovídá 503 a loguje chybu `abuse-file-full`; soubor je pak potřeba zpracovat a vyprázdnit. Hlášení obsahuje jen datum UTC, id a upravený důvod, čas dne v něm není. Hlášení starší než 14 dní server smaže při každém úklidu a před každým novým hlášením.

Paměť: bez proměnné `GOMEMLIMIT` si `nylzod serve` nastaví měkký limit paměti 192 MiB, aby se ověřování PoW a čekající uploady vešly do kontejneru s 256 MB. Vlastní `GOMEMLIMIT` má přednost. Mezipaměť SQLite leží mimo tento limit, proto je omezená na 512 KiB na spojení. Přes nešifrované HTTP/2 (h2c) drží server nejvýš 64 KiB nepřečteného těla na stream a 1 MiB na spojení.

Soubory databáze (`nylzo.db` a jeho `-wal` a `-shm`) vznikají s právy 0600. Existující soubor server nepřenastavuje, proto adresář dat nedávejte do sdíleného umístění. Smazané řádky SQLite přepisuje nulami (`secure_delete`), WAL server po každém úklidu zkrátí na nulu a databázi ze starší verze při prvním otevření jednou zkompaktuje (`VACUUM`). Čtení blobu nemění čas přístupu k souboru (na Linuxu `O_NOATIME`); svazek s daty přesto připojte s `noatime`. Co si po smazání pamatuje souborový systém nebo poskytovatel úložiště, server neovlivní.

Velká zpráva má na nahrání i stažení čas podle velikosti: nejméně 120 s, jinak tolik, kolik trvá přenos při 128 kbit/s (u největšího objektu asi 5,6 minuty). Stejně dlouho platí lease, takže příjemce na pomalé lince o zprávu nepřijde.

Pokusy o passphrase se počítají na zprávu, ne na adresu. Prvních 5 chybných pokusů je bez čekání, potom se čeká 2^(n − 5) s (nejvýš 1 h) zaokrouhleno nahoru na celou minutu. Po 100 chybných pokusech za celou životnost je zpráva zamčená do konce platnosti a odesílatel to vidí ve status. Klienti musí odmítnout passphrase kratší než 12 znaků. Žádný čas v databázi není přesnější než na minutu, proto zpráva i lease žijí o 0 až 60 s déle, než bylo nastaveno.

Po signálu SIGTERM server 30 s čeká na rozběhnuté požadavky, pak zbylá spojení zavře a skončí s kódem 0.

Id zprávy vydává server: `GET /api/v1/instance` vrací v poli `create` náhodné id a token platný 10 minut a vytvoření bez platného tokenu skončí 403 `id_token`. Ukončená zpráva zmizí i s náhrobkem 7 dní po konci, ale její id už nikdo znovu nezíská, takže pod starým odkazem nejde vytvořit nový obsah. Vydaná id server neukládá.

Klient nikdy neposílá R, V ani M. Jsou to semínka klíčů Ed25519: při vytvoření klient pošle jejich veřejné klíče a každý další požadavek podepíše nad jednorázovou výzvou z `GET /api/v1/challenge` (platí 60 s). Server ukládá jen HMAC veřejných klíčů s pepperem. Výzvu spotřebuje první požadavek, který ji předloží, ať server odpoví čímkoli, takže zachycený požadavek nejde zopakovat, ani když ho server předtím odmítl kvůli limitu. Neplatný podpis, prošlá nebo použitá výzva i neznámé id dostanou stejnou odpověď 404. Veřejný klíč, který není kanonickým bodem křivky, má malý řád nebo se shoduje s jiným klíčem téže zprávy, server při vytvoření odmítne s 400. Podrobnosti jsou ve [spec §5.5](spec/protocol.md#55-request-signatures).

Logy obsahují čas, metodu, vzor cesty a stav, u všech cest kromě vytvoření, claimu a blobu i latenci a velikost odpovědi; u těch tří obojí sleduje velikost zprávy. Id zprávy v nich není. Nikdy neobsahují surovou cestu, cestu k souboru blobu, hlavičku `Authorization`, hlavičky `X-Nylzo-*`, těla ani adresy klientů. Chyby úložiště se logují jen jako operace a systémová chyba. Při velmi malém provozu může čas řádku sedět na minutu vytvoření nebo otevření některé zprávy. Interní chyby `net/http` se logují jen jako kategorie bez textu.

`nylzod sweep-once` skončí nenulovým kódem, když některý krok úklidu selže. Vestavěný úklid běží i hned po startu. Denní kontrola osiřelých blobů se zopakuje při dalším úklidu, dokud se jí nepodaří vypsat řádky i úložiště blobů. Burn a cancel si před smazáním blobu zapíšou zamýšlený stav. Expirovaná zpráva zápis nepotřebuje, protože ji ukončuje samo `expires_at`. Po pádu procesu konec dokončí další požadavek nebo úklid. Chybějící blob u neexpirované zprávy bez zapsaného stavu server hlásí jako chybu 500, zprávu nesmaže a claim kvůli němu nespotřebuje otevření.

Při upgradu zastavte staré procesy dřív, než nový `nylzod` otevře databázi; přidá do ní sloupce `ending` a `next_attempt_at`, odstraní tabulku `passphrase_attempts` a sloupce `day_fail_count` a `day_fail_date`, přestaví tabulky `messages`, `tombstones` a `rate_counters` bez rowid, smaže z `rate_counters` čítače podle adres a jednou spustí `VACUUM`. Starší verze s takovou databází nepracují, protože jejich úklid maže podle rowid, proto se po tomto upgradu ke starší verzi nevracejte. Zprávy vytvořené verzí, která ještě posílala R, V a M jako tokeny, po upgradu nejde otevřít ani spravovat; vyprší nejpozději za 7 dní.

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
