<h1>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/brand/dark/nylzo-landscape-dark.svg">
    <img alt="Nylzo" src="assets/brand/light/nylzo-landscape-light.svg" width="301" height="105">
  </picture>
</h1>

Nylzo je open-source služba pro jednorázové sdílení citlivých zpráv a souborů do 5 MB. Obsah se šifruje v klientovi, server dostává jen ciphertext a HMAC důkazy; odkaz nese klíč jen ve fragmentu URL, který prohlížeč na server nikdy neposílá. Návrh a jeho zdůvodnění jsou v `docs/designs/nylzo-architecture-research.md`, normativní zápis protokolu v1 je v `spec/protocol.md`.

Tento repozitář obsahuje protokol v1, jádro v Go a v TypeScriptu svázané společnými vektory a jednu binárku `nylzo`, která je zároveň příkazová řádka i server s úložištěm SQLite a disk a s vloženým webovým klientem (D53). Webový klient ve Svelte 5 (plány v `docs/plans/`) vytvoří a otevře zprávu s textem nebo souborem, volitelně s passphrase, a umí PoW. Odesílatel na `/sent` vidí stav zpráv zapamatovaných na zařízení a může je zrušit, příjemce může zprávu smazat a kdokoli s odkazem ji může nahlásit na `/report`. Stránka `/transparency` ukazuje, co instance ukládá a loguje, její limity, provozovatele a otisky souborů webového klienta. Úvodní stránka a stránka pro otevření zprávy mají vzhled Nebula (`docs/plans/2026-10-24-nebula-web-client.md`), ostatní obrazovky sdílejí jejich rám. Vydání se sestavují reprodukovatelně a podepisují (`.github/workflows/release.yml`) a `deploy/compose` obsahuje příklad nasazení s Caddy. Adaptér postgres+s3 zatím není součástí.

## Rozložení repozitáře

| Cesta | Obsah |
|---|---|
| `spec/protocol.md` | normativní protokol v1 |
| `spec/vectors.json` | testovací vektory generované z Go jádra |
| `internal/core` | Go jádro: odvození klíčů, KDF profily, obálka, meta JSON, odkaz |
| `internal/pepper` | serverové klíče podle účelu z pepperu a rotace |
| `internal/store` | rozhraní úložiště, adaptér `sqlite` a `disk`, sdílená testovací sada |
| `internal/server` | HTTP server: endpointy, limity, PoW, hlavičky, úklid |
| `cmd/nylzo`, `internal/cli`, `internal/client` | binárka `nylzo`: příkazy `create`, `read`, `status`, `cancel`, `report`, `burn`, `serve`, `sweep-once`, `takedown`, `verify-client`, `version` a HTTP klient nad jádrem |
| `skills/nylzo` | skill pro agenty (Agent Skills): jak poslat a otevřít tajemství přes CLI, aby nezůstalo v chatu (D70) |
| `cmd/nylzo-vectors` | generátor vektorů |
| `web/core` | TypeScript jádro `@nylzo/core` (Web Crypto, scrypt z `@noble/hashes`, PoW) |
| `web/app` | webový klient `@nylzo/web` (Svelte 5, Vite 8, Tailwind 4): HTTP klient, toky vytvoření a čtení, worker pro scrypt a PoW, obrazovky ve vzhledu Nebula a slovník cs a en |
| `web/e2e` | testy Playwright proti `nylzo serve` se vloženým klientem, včetně výměny zpráv mezi CLI a prohlížečem |
| `internal/server/web` | zástupná stránka; `dist/` je výstup sestavení `web/app`, který `nylzo` vkládá |
| `deploy/compose` | příklad self-hostu: `nylzo serve` za Caddy s TLS, pepper jako Docker secret |
| `assets/brand` | oficiální logo: SVG ve světlé a tmavé variantě a barvy v `manifest.json` (D72) |
| `.goreleaser.yaml`, `.github/workflows/release.yml` | vydání: binárky, image na ghcr.io, podpisy, SBOM a provenance |

## Rychlý start s mise

S [mise](https://mise.jdx.dev/getting-started.html) stačí v kořeni repozitáře tři příkazy. mise nejdřív chce, abyste konfiguraci repozitáře výslovně povolili, protože nastavuje prostředí a spouští úlohy; přečtěte si `mise.toml` a povolte ji:

```bash
mise trust
```

Další příkaz stáhne Go, Node.js a pnpm ve verzích z `mise.toml` a ověří je proti checksumům v `mise.lock`, poslední nainstaluje závislosti webu, sestaví webového klienta a binárku `bin/nylzo` a pustí server na `http://localhost:8080`:

```bash
mise install
```

```bash
mise run dev
```

Pak v prohlížeči napište zprávu, zkopírujte odkaz a otevřete ho v anonymním okně. Data a pepper leží v `.dev/`, takže zprávy přežijí restart; `mise run dev:reset` je smaže. Server naslouchá vždy jen na `127.0.0.1` (jiný port nastaví `NYLZO_DEV_PORT=8081`; 8090 si berou e2e testy) a má vypnuté limity vytváření a čtení, aby zkoušení nenarazilo na 20 zpráv za hodinu. Pepper z prostředí shellu (`NYLZO_PEPPER` a spol.) úloha ignoruje. Z telefonu přes adresu v lokální síti to nepůjde, Web Crypto potřebuje HTTPS nebo localhost.

Pro práci na webovém klientovi pusťte vedle `mise run dev` ještě `mise run dev:web`: Vite s hot reloadem na `http://localhost:5173` posílá `/api` na server z `mise run dev` (jinou adresu nastaví `NYLZO_DEV_API`). Vite stránky neposílá s CSP serveru, takže CSP a nonce ověříte až v `mise run dev` nebo v e2e testech.

Další úlohy vypíše `mise tasks`: `mise run test` (Go s race detektorem a TypeScript), `mise run lint`, `mise run build`, `mise run e2e` (prohlížeče jednou stáhne `mise run e2e:browsers`). `test` a `lint` spouštějí stejné kontroly jako joby Go a TypeScript v CI. Úlohy potřebují POSIX shell, tedy Linux, macOS nebo WSL. Na Linuxu potřebuje race detektor překladač C (na Debianu a Ubuntu `sudo apt install gcc`) a prohlížeče pro e2e systémové knihovny, které nainstaluje `sudo pnpm --dir web/e2e exec playwright install-deps`. Proč mise a co hlídá CI, popisuje `docs/plans/2026-10-24-dev-setup-mise.md` (D58, D59).

## Požadavky bez mise

- Go 1.27.3, připnuté řádkem `toolchain` v `go.mod`.
- Node.js 24.22.1 podle `web/.node-version` a pnpm 12 (pole `packageManager` ve `web/package.json` ho přes corepack stáhne samo).
- Stejné verze stojí v `mise.toml`; CI hlídá, že se neliší.

## Spuštění serveru lokálně se SQLite

Server potřebuje 32bajtový pepper, tajemství mimo databázi, kterým podepisuje uložené tokeny. Vygenerujte ho a spusťte server:

```bash
export NYLZO_PEPPER="$(openssl rand -hex 32)"
go run ./cmd/nylzo serve
```

Server naslouchá na `:8080`, data ukládá do `./data` (soubor `nylzo.db` a adresář `blobs`). Pepper si uložte; bez něj server žádný existující token neověří a živé zprávy zaniknou.

Kontrola, že běží:

```bash
curl -s http://localhost:8080/api/v1/instance
```

Stránky `/`, `/sent`, `/report` a `/m/{id}` servírují webového klienta, pokud je před sestavením serveru sestavený do `internal/server/web/dist`; jinak servírují zástupné HTML. Klienta sestavíte takto a server pak vložte znovu přes `go run` nebo `go build`:

```bash
cd web && pnpm install --frozen-lockfile && pnpm --filter @nylzo/web build
```

Úklid běží uvnitř serveru každých 5 minut. Pro nasazení s externím plánovačem nastavte `NYLZO_SWEEP_INTERVAL=0` a spouštějte:

```bash
go run ./cmd/nylzo sweep-once
```

Příkaz `sweep-once` pepper nepotřebuje. Databázi nemigruje: když má starší schéma, skončí chybou a migraci provede až `nylzo serve`, takže plánovač nikdy nezmění schéma pod běžícím starším serverem. Image plánovače proto aktualizujte až po serveru.

Serverové příkazy `nylzo` jsou `serve`, `sweep-once` a `takedown`; žádný nebere argumenty ani přepínače, konfiguraci čtou z proměnných prostředí a `takedown` navíc id zpráv ze stdin. `nylzo` bez příkazu vypíše nápovědu. Neznámý příkaz nebo argument vypíše nápovědu na standardní chybový výstup a skončí kódem 2, chyba za běhu kódem 1. Chybu konfigurace server zaloguje i s názvem proměnné. Server běží na Linuxu a macOS; na Windows `serve` odmítne start, protože tam chybí fsync adresáře i kontrola volného místa (D54), a doporučí WSL nebo Docker.

### Proměnné prostředí

| Proměnná | Výchozí | Význam |
|---|---|---|
| `NYLZO_PEPPER` | povinná pro `serve` | 32 bajtů v hex nebo base64url; pepper ze samých stejných bajtů server odmítne |
| `NYLZO_PEPPER_PREVIOUS` | prázdná | předchozí pepper při rotaci; starší řádky se ověřují jím, nové se zapisují aktuálním. Po 7 dnech už žádná živá zpráva starý pepper nepotřebuje. Náhrobky ho drží ještě 7 dní a vyexpirovaná zpráva se stane náhrobkem až při dalším požadavku nebo úklidu, takže pro úplné stavy ho nechte 14 dní a jeden interval úklidu. Čítače limitů na adresu drží každý proces v paměti, takže rotace na ně nemá vliv; backoff passphrase, celoživotní strop 100 chybných pokusů a limit hlášení na zprávu jsou v databázi a platí dál |
| `NYLZO_PEPPER_FILE`, `NYLZO_PEPPER_PREVIOUS_FILE` | prázdná | cesta k souboru s pepperem místo proměnné (D44), třeba Docker secret; koncový konec řádku se ignoruje. Proměnná a soubor pro tutéž hodnotu zároveň jsou chyba |
| `NYLZO_LISTEN`, `PORT` | `:8080` | adresa naslouchání |
| `NYLZO_DATA_DIR` | `data` | adresář pro SQLite a bloby; souborový systém musí podporovat hard linky a fsync adresáře (`serve` i `sweep-once` to ověří při startu), takže FAT, exFAT a FUSE připojení objektových úložišť nefungují. `sweep-once` adresář nezakládá: bez existující databáze a adresáře `blobs` skončí chybou, proto mu v plánovači dejte absolutní cestu |
| `NYLZO_TRUSTED_PROXIES` | prázdná | CIDR proxy, od kterých se bere `X-Forwarded-For` |
| `NYLZO_PROXY_MODE` | prázdná | `scaleway` čte `X-Real-IP`, jen když ji požadavek nese právě jednou; při nastaveném `NYLZO_TRUSTED_PROXIES` jen ze spojení z těchto adres. Bezpečné jen za ingressem, přes který jde každé spojení |
| `NYLZO_SWEEP_INTERVAL` | `5m` | interval vestavěného úklidu, `0` vypne; záporná hodnota je chyba |
| `NYLZO_MAX_STREAMS` | `64` | nejvýš tolik odpovědí claim a blob streamuje naráz; další dostanou 503 s `Retry-After: 5` a otevření nespotřebují. Jedna adresa (/32, /64) drží nejvýš 2 sloty a jeden rozsah (/24, /48) nejvýš 8 |
| `NYLZO_MIN_FREE_BYTES` | `1073741824` (1 GiB) | když by volné místo souborového systému s daty po odečtení právě nahrávaných zpráv a nové zprávy kleslo pod tuto hodnotu, vytvoření odpoví 503, aby zbylo místo na zápisy, které potřebují burn a zrušení; `0` kontrolu vypne |
| `NYLZO_MAX_FILE_BYTES` | `5242880` | limit souboru; lze jen snížit, protokolové maximum vynucují obě jádra |
| `NYLZO_MAX_TTL`, `NYLZO_DEFAULT_TTL` | `168h`, `24h` | platnost zprávy; maximum je 7 dní, minimum 60 s |
| `NYLZO_MAX_OPENS` | `10` | maximum otevření, nejvýš 100 |
| `NYLZO_DEFAULT_KDF` | `1` | výchozí profil passphrase hlášený klientům (1 scrypt, 2 PBKDF2); jiné hodnoty server odmítne. Scrypt trvá na iPhonu 15 Pro Max přes HTTPS zhruba 0,55 s (D33, `docs/research/od6-scrypt-safari.md`) |
| `NYLZO_RATE_CREATE_HOUR`, `NYLZO_RATE_CREATE_DAY`, `NYLZO_RATE_CREATE_BYTES_DAY` | `20`, `100`, `50000000` | limity vytváření na kyblík /32 nebo /64; na /24 nebo /48 platí pětinásobek. U všech `NYLZO_RATE_*_HOUR`, `_DAY` a `_BYTES_DAY` znamená `0` bez limitu, což se hodí jen pro lokální vývoj |
| `NYLZO_RATE_COARSE_MULTIPLIER` | `5` | násobek limitů pro hrubý kyblík /24 nebo /48. Požadavek, který odmítne limit /32 nebo /64, se do limitu /24 nebo /48 nezapočítá |
| `NYLZO_RATE_READ_HOUR` | `300` | metadata, claim a blob na kyblík |
| `NYLZO_RATE_ABUSE_HOUR` | `5` | hlášení zneužití na kyblík |
| `NYLZO_RATE_SOURCE_RPS`, `NYLZO_RATE_SOURCE_BURST` | `10`, `20` | paměťový token bucket jednoho zdroje (kyblík /32 nebo /64) pro `/api/*`. Za ním následuje bucket rozsahu /24 nebo /48 s polovinou kapacity instance, takže jeden zdroj ani jeden rozsah nevezme víc než polovinu bucketu instance |
| `NYLZO_RATE_BUCKET_RPS`, `NYLZO_RATE_BUCKET_BURST` | `50`, `100` | paměťový token bucket instance pro `/api/*` |
| `NYLZO_GLOBAL_DAILY_CREATES` | `0` (vypnuto) | denní strop vytvoření; od 50 % vyžaduje PoW, při 100 % vrací 503. Místo se rezervuje atomicky až po PoW a při každém selhání se vrátí, takže souběh strop nepřekročí. Dokud uložené zprávy samy strop nezaplní, 503 radí zkusit to za 60 s. Platí to i pro místa, která drží nahrávání přerušené pádem procesu, takže rada na 60 s se může opakovat až do konce dne UTC. Jakmile strop zaplní uložené zprávy, radí počkat do dalšího dne |
| `NYLZO_OPERATOR`, `NYLZO_CONTACT`, `NYLZO_HOSTING` | prázdné | provozovatel, kontakt (e-mail nebo adresa `https://`) a hosting pro stránku `/transparency` (D62). Každá nejvýš 200 bajtů bez řídicích znaků; jinak server nenaběhne. Nenastavené stránka uvede jako neuvedené |
| `NYLZO_POW_DIFFICULTY`, `NYLZO_POW_CONCURRENCY` | `4`, `4` | obtížnost (nejvýš 8) a souběh ověření PoW (každé ověření drží 16 MiB; strop souběhu je 4). Každou výzvu lze použít jen jednou |

Za reverzní proxy nastavte `NYLZO_TRUSTED_PROXIES` nebo `NYLZO_PROXY_MODE`. Bez nich server vidí jen adresu proxy a všichni klienti sdílejí jeden kyblík, včetně limitu na zdroj. Server na to upozorní varováním `forwarded-header-ignored` v logu, nejvýš jednou za hodinu a jen s názvem hlavičky: požadavek nesl `X-Forwarded-For`, `X-Real-IP` nebo `Forwarded`, ale server přesto vzal adresu spojení, protože proxy není mezi důvěryhodnými, posílá jen `X-Real-IP` nebo `Forwarded`, nebo v režimu `scaleway` chybí `X-Real-IP`. Totéž způsobí klient, který hlavičku pošle sám mimo důvěryhodnou proxy.

Všechny limity na kyblík adresy drží každý proces jen v paměti, s klíčem, který si vylosuje při startu. Databáze, záloha, logy ani pepper proto o adresách klientů nic neobsahují. Limity na adresu ale platí pro jednu instanci a po restartu nebo probuzení kontejneru začínají od nuly. Globální denní strop, PoW, backoff passphrase a limit hlášení na zprávu jsou v databázi a platí napříč instancemi.

Hodnota `0` u proměnných `NYLZO_RATE_*_HOUR`, `NYLZO_RATE_*_DAY` a `NYLZO_RATE_COARSE_MULTIPLIER` limit vypíná, neblokuje.

Hlášení zneužití podepsaná klíčem čtení zprávy se zapisují pro ruční posouzení do souboru na každý den UTC, `data/abuse/RRRR-MM-DD.jsonl`; server do něj jen připisuje. Hlášení musí mít `Content-Type: application/json`. Server důvod hlášení dekóduje z procentového kódování, řídicí znaky nahradí mezerou, neviditelné formátovací znaky (třeba přepínače směru textu) zahodí, vymaže fragmenty odkazů a dlouhé tokeny a důvod zkrátí. Hlášení o už ukončené zprávě přijme, ale neuloží. Na jednu zprávu uloží nejvýš 3 hlášení denně. Limit platí jen na zprávu, aby žádný řádek nespojoval zprávu s adresou, a jeden ohlašovatel ho proto může vyčerpat. Když by hlášení zvětšilo soubory dohromady nad 16 MiB, server na hlášení, které by uložil, odpovídá 503 a loguje chybu `abuse-file-full`; soubory je pak potřeba zpracovat a smazat. Hlášení obsahuje jen datum UTC, id a upravený důvod, čas dne v něm není a čas změny souboru server nastaví na začátek dne. Soubory starší než 7 dní server smaže při každém úklidu, i při `sweep-once`, a před každým novým hlášením.

Po posouzení ukončí provozovatel zprávu příkazem `takedown` na stroji se serverem (D63). Bere id ze stdin, jedno na řádek, s `NYLZO_DATA_DIR` jako `sweep-once`, a každou zprávu ukončí stejně jako burn, takže příjemce uvidí, že byla smazána, a odesílatel na `/sent` stav „smazal ji příjemce, nebo ji po hlášení odstranil provozovatel“. Když některý řádek není id, třeba vložený odkaz s klíčem, odmítne celý vstup a nic nezmění. Pro každé id vypíše, jak dopadlo. Do logu serveru nic nezapíše.

```bash
jq -r .id data/abuse/2026-10-24.jsonl | sort -u > k-posouzeni.txt
```

```bash
nylzo takedown < ukoncit.txt
```

Paměť: bez proměnné `GOMEMLIMIT` si `nylzo serve` nastaví měkký limit paměti 192 MiB, aby se ověřování PoW a čekající uploady vešly do kontejneru s 256 MB. Vlastní `GOMEMLIMIT` má přednost. Mezipaměť SQLite leží mimo tento limit, proto je omezená na 512 KiB na spojení. Přes nešifrované HTTP/2 (h2c) drží server nejvýš 64 KiB nepřečteného těla na stream a 1 MiB na spojení.

Soubory databáze (`nylzo.db` a jeho `-wal` a `-shm`) vznikají s právy 0600. Existující soubor server nepřenastavuje, proto adresář dat nedávejte do sdíleného umístění. Smazané řádky SQLite přepisuje nulami (`secure_delete`). Každý commit hned vrátí z WAL do databáze (`wal_autocheckpoint = 1`), takže WAL drží jen poslední transakci a neukáže pořadí dřívějších zápisů. Stojí to tři synchronizace disku navíc na každou zápisovou transakci (na Linuxu čtyři `fsync` místo jednoho). Zápis dělá každý podepsaný požadavek, protože spotřebuje výzvu, takže na pomalém disku, třeba SD kartě nebo síťovém svazku, tohle určuje, kolik požadavků za sekundu server zvládne. Dočasná data SQLite zůstávají v paměti (`temp_store = MEMORY`), takže kopie řádků nikdy nevznikne v dočasném adresáři mimo adresář dat. Po každém úklidu server WAL zkrátí na nulu a databázi ze starší verze při prvním otevření jednou zkompaktuje (`VACUUM`). Replikaci WAL, třeba Litestream, nepoužívejte: brání checkpointům serveru a stránky WAL ukládá do repliky, kde by zůstaly smazané řádky i pořadí zápisů. Čtení blobu nemění čas přístupu k souboru (na Linuxu `O_NOATIME`); svazek s daty přesto připojte s `noatime`. Co si po smazání pamatuje souborový systém nebo poskytovatel úložiště, server neovlivní.

Velká zpráva má na nahrání i stažení čas podle velikosti: nejméně 120 s, jinak tolik, kolik trvá přenos při 128 kbit/s (u největšího objektu asi 5,6 minuty). Stejně dlouho platí lease, takže příjemce na pomalé lince o zprávu nepřijde.

Pokusy o passphrase se počítají na zprávu, ne na adresu. Prvních 5 chybných pokusů je bez čekání, potom se čeká 2^(n − 5) s (nejvýš 1 h) zaokrouhleno nahoru na celou minutu. Po 100 chybných pokusech za celou životnost je zpráva zamčená do konce platnosti a odesílatel to vidí ve status. Klienti musí odmítnout passphrase kratší než 12 znaků. Žádný čas v databázi není přesnější než na minutu, proto zpráva i lease žijí o 0 až 60 s déle, než bylo nastaveno.

Po signálu SIGTERM server 30 s čeká na rozběhnuté požadavky, pak zbylá spojení zavře a skončí s kódem 0.

Id zprávy vydává server: `GET /api/v1/instance` vrací v poli `create` náhodné id a token platný 10 minut a vytvoření bez platného tokenu skončí 403 `id_token`. Ukončená zpráva zmizí i s náhrobkem 7 dní po konci, ale bez pepperu její id už nikdo znovu nezíská, takže pod starým odkazem nejde vytvořit nový obsah. Kdo pepper ukradne, token pro libovolné id vystaví sám (spec §13). Vydaná id server neukládá.

Klient nikdy neposílá R, V ani M. Jsou to semínka klíčů Ed25519: při vytvoření klient pošle jejich veřejné klíče a každý další požadavek podepíše nad jednorázovou výzvou z `GET /api/v1/challenge` (platí 60 s). Server ukládá jen HMAC veřejných klíčů s pepperem. Výzvu spotřebuje první požadavek, který ji předloží, ať server odpoví čímkoli, takže zachycený požadavek nejde zopakovat, ani když ho server předtím odmítl kvůli limitu. Neplatný podpis, prošlá nebo použitá výzva i neznámé id dostanou stejnou odpověď 404. Veřejný klíč, který není kanonickým bodem křivky, má malý řád nebo se shoduje s jiným klíčem téže zprávy, server při vytvoření odmítne s 400. Kanonický tvar a řád klíče kontroluje i u každého podepsaného požadavku. Spotřeby výzev, které přijdou během běžícího zápisu, zapíše v další transakci společně, nejvýš 256 v jedné; když se nic nezapisuje, zapíše spotřebu hned. Podrobnosti jsou ve [spec §5.5](spec/protocol.md#55-request-signatures).

Logy obsahují čas zaokrouhlený na minutu, metodu, vzor cesty a stav, u všech cest kromě vytvoření, claimu a blobu i latenci; u těch tří latence sleduje velikost zprávy. Velikost odpovědi v logu není nikde, protože u metadat a status prozrazuje počty číslic v hodnotách zprávy. Id zprávy v nich není. Nikdy neobsahují surovou cestu, cestu k souboru blobu, hlavičku `Authorization`, hlavičky `X-Nylzo-*`, těla ani adresy klientů. Chyby úložiště se logují jen jako operace a systémová chyba. Při velmi malém provozu může čas řádku sedět na minutu vytvoření nebo otevření některé zprávy. Interní chyby `net/http` se logují jen jako kategorie bez textu.

`nylzo sweep-once` skončí nenulovým kódem, když některý krok úklidu selže. Vestavěný úklid běží i hned po startu. Denní kontrola osiřelých blobů se zopakuje při dalším úklidu, dokud se jí nepodaří vypsat řádky i úložiště blobů. Burn a cancel si před smazáním blobu zapíšou zamýšlený stav. Expirovaná zpráva zápis nepotřebuje, protože ji ukončuje samo `expires_at`. Po pádu procesu konec dokončí další požadavek nebo úklid. Chybějící blob u neexpirované zprávy bez zapsaného stavu server hlásí jako chybu 500, zprávu nesmaže a claim kvůli němu nespotřebuje otevření.

Při upgradu zastavte staré procesy dřív, než nový `nylzo serve` otevře databázi; přidá do ní sloupce `ending` a `next_attempt_at`, odstraní tabulku `passphrase_attempts` a sloupce `day_fail_count` a `day_fail_date`, přestaví tabulky `messages`, `tombstones` a `rate_counters` bez rowid, smaže z `rate_counters` čítače podle adres a jednou spustí `VACUUM`. Databáze dostane verzi schématu 4 se spouštěči, které zastaví zápisy starších verzí: čítače podle adres, nonce PoW s časem a lease s pořadovým číslem. Starší verze proto s upgradovanou databází selže při vytvoření, metadatech, claimu, stažení blobu i hlášení zneužití. Databázi s novější verzí schématu server odmítne otevřít. Po upgradu se ke starší verzi nevracejte. Zprávy vytvořené verzí, která ještě posílala R, V a M jako tokeny, po upgradu nejde otevřít ani spravovat; vyprší nejpozději za 7 dní.

## Příkazová řádka

Stejná binárka `nylzo` posílá a čte zprávy z příkazové řádky (D53, D55). Odkaz, passphrase ani správcovský token nikdy nejsou v argumentech, kde by je viděl každý proces přes `ps` a uložila by je historie shellu: přicházejí ze stdin, ze souboru, z proměnné `NYLZO_*` nebo z terminálu bez echa. Argument, který vypadá jako odkaz s klíčem, `nylzo` odmítne dřív, než cokoli pošle. Se serverem mluví jen přes HTTPS. Výjimkou je přesně `localhost` a adresa loopback (127.0.0.1, ::1); takové spojení nejde přes proxy a `nylzo` ověří, že opravdu míří na loopback. Přesměrování nenásleduje. Chybové hlášky nikdy neopakují hodnotu přepínače ani cestu k souboru, aby se tajemství omylem vložené na jejich místo neocitlo v logu. Text od serveru nebo z certifikátu do nich `nylzo` nepouští, a co v hlášce zůstane, vypíše s řídicími znaky jako `\u001b`, takže server ani útočník v síti nepřepíše terminál.

```bash
printf '%s' 'heslo k databázi' | nylzo create --server https://nylzo.example --ttl 1h --manage-token-file spravce.txt
```

`create` vypíše jen odkaz. Text bere ze stdin (z terminálu jako jeden skrytý řádek), přílohu z `--from-file`. Passphrase přidá `--passphrase-file soubor` (první řádek), `NYLZO_PASSPHRASE_FILE` nebo `--ask-passphrase` (dvakrát z terminálu bez echa); profil zvolí `--kdf scrypt|pbkdf2`. Server lze zadat i proměnnou `NYLZO_SERVER`. Správcovský token pro stav a zrušení zapíše jen `--manage-token-file` (nový soubor s právy 0600) nebo `--print-manage-token`; má dva řádky, adresu zprávy bez části za `#` a token, a zprávu s ním přečíst nejde.

```bash
nylzo read --link-file odkaz.txt
nylzo status --manage-token-file spravce.txt
nylzo cancel --manage-token-file spravce.txt
nylzo burn < odkaz.txt
```

`read` a `burn` berou odkaz ze stdin, z `--link-file` nebo z `NYLZO_LINK`; když použijí proměnnou, napíšou to na standardní chybový výstup. Odkaz z novější verze protokolu odmítnou s radou stáhnout novější `nylzo`, místo aby ho označily za poškozený. `read` si ještě před otevřením zprávy založí nový adresář s právy 0700 v dočasném adresáři (nebo uvnitř `--out`), zapíše do něj text jako `message.txt` a přílohu pod očištěným názvem, soubory s právy 0600, a vypíše jejich cesty, takže žádná kolize názvů nespotřebuje otevření zbytečně. Když zápis selže, zkusí druhý nový adresář vedle prvního, nikdy jinde. Text na standardní výstup jen s `--stdout`; když jde výstup do terminálu a text obsahuje řídicí znaky, zapíše ho místo toho do souboru. U zprávy s passphrase se zeptá v terminálu, nebo vezme `--passphrase-file`; Ctrl-C během skrytého dotazu vrátí terminálu echo a `nylzo` ukončí. Když se stahování přeruší, zkusí ho v témže běhu znovu přes lease a další otevření nespotřebuje. `status` (s `--json` pro skripty) a `cancel` berou správcovský token ze stdin, z `--manage-token-file` nebo z `NYLZO_MANAGE_TOKEN_FILE`.

```bash
nylzo verify-client --server https://nylzo.example
```

`verify-client` porovná webového klienta, kterého server posílá, s klientem vloženým v této binárce (D61). Stáhne každou stránku a každý soubor dvakrát, bez komprese a tak, jak o ně žádá prohlížeč, a ověří obsah, CSP s nonce i bezpečnostní hlavičky a manifest `/.well-known/nylzo/client.json`. Hlavičku, kterou tato verze neposílá, bere jako neshodu; hlavičku, kterou přidává proxy nebo CDN před serverem, povolí `--allow-header Název` (opakovaně), kromě hlaviček, které mění chování stránky, třeba `Refresh` nebo `Set-Cookie`. Server musí běžet na stejné verzi jako binárka, jinak příkaz skončí kódem 1 a řekne, kterou verzi použít. Ověří jen to, co server poslal tomuto příkazu. Server ho pozná podle hlaviček a otisku TLS a může prohlížečům posílat něco jiného, takže kontrola odhalí záměnu z napadeného sestavení nebo chybného nasazení, ne server, který ji úmyslně skrývá. Má smysl pouštět ji pravidelně a z jiné sítě. Binárka sestavená bez webového klienta nemá s čím porovnat a skončí chybou. Workflow `monitor` totéž dělá každou hodinu z GitHub Actions binárkou z ověřeného vydání, jakmile vlastník nastaví proměnnou repozitáře `NYLZO_MONITOR_URL` (D68, AK25).

```bash
nylzo report --link-file odkaz.txt --reason-file duvod.txt
```

`report` nahlásí zprávu provozovateli stejně jako stránka `/report`: odkaz bere ze stdin, z `--link-file` nebo z `NYLZO_LINK` a podepíše hlášení klíčem čtení. Důvod bere ze souboru `--reason-file`, ne z argumentu, protože vložený odkaz by v argumentu viděl každý proces přes `ps` (AK14). Před odesláním z něj odstraní odkazy a dlouhé kódy stejně jako webový klient a server (spec §6.6) a stejně jako webový klient odmítne důvod, který je i potom delší než 1000 bajtů. Hlášení zprávy, která už skončila, server přijme, ale neuloží.

Agentům (Claude Code a dalším, kteří umějí Agent Skills) říká skill `skills/nylzo/SKILL.md`, jak tajemství poslat a otevřít jen přes `nylzo`: tajemství i odkaz v souborech v soukromém adresáři mimo repozitář, nikdy v chatu ani v argumentu, nikdy WebFetch nebo curl na odkaz, a dešifrovaný obsah předat programu přímo ze souboru, ne modelu (D70). Do Claude Code ho přidáte zkopírováním adresáře `skills/nylzo` do `~/.claude/skills/` nebo do `.claude/skills/` projektu. Skill je jen návod; vynucení dělají výchozí chování CLI, oprávnění a sandbox.

Klíče a passphrase `nylzo` po použití přepíše nulami, ve vlastních kopiích i v kopiích, které dělá jádro při odvození klíčů a podpisu. V Go je to jen snaha: passphrase jako řetězec přepsat nejde, knihovny a úklid paměti mohou nechat další kopie a co jednou prošlo terminálem nebo rourou, mohlo zůstat jinde. Na Windows práva 0700 a 0600 přístup neomezují: soubory dědí oprávnění cílového adresáře. Výchozí dočasný adresář uživatele je soukromý, ale `--out` na sdílené složce zprávu zpřístupní i ostatním. Návratové kódy: 0 úspěch, 1 chyba, 2 špatné použití včetně tajemství v argumentu.

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

`svelte-check` 4.7.7 podporuje TypeScript nejvýš 6, proto `web/app` kontroluje typy přes TypeScript 6.0.3; `web/core` a `web/e2e` používají TypeScript 7.

E2e testy sestaví `nylzo` z aktuálního kódu a spustí ho s dočasným adresářem dat. Potřebují sestaveného klienta a prohlížeče Playwright; Go hledají v `PATH`, nebo v proměnné `NYLZO_GO`:

```bash
cd web && pnpm --filter @nylzo/web build && cd e2e && pnpm exec playwright install chromium firefox webkit && pnpm e2e
```

Job „Compose with Caddy“ v CI spustí `deploy/compose` pro `localhost` s image sestaveným stejně jako vydání (D67). Ověří, že 5MiB soubor projde přes Caddy tam i zpět (AK17), že Caddy vyjedná X25519MLKEM768 (AK26, nástroj `tools/tlscheck`), že `nylzo verify-client` přes Caddy najde nezměněného webového klienta, že při 502 nezapíše id, cestu ani hlavičky `X-Nylzo-*` (spec §10) a že podvržené `X-Forwarded-For` nedostane vlastní limit (AK12). Lokálně ho nespouštějte na stroji, kde porty 80 a 443 něco používá.

Fuzz cíle (obálka, plaintext obálky, meta JSON, odkaz, fragment, hlavička vytvoření); CI každý z nich spouští 15 s:

```bash
go test -run='^$' -fuzz='^FuzzOpen$' -fuzztime=30s ./internal/core
```

Lint a modernizace kódu, stejně jako v CI (nastavení je v `.golangci.yml`):

```bash
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...
```

```bash
go fix -diff ./...
```

Benchmarky kritických cest (ověření podpisu, čtení řádku, dávková spotřeba výzev, podepsaný požadavek):

```bash
go test -run='^$' -bench=. ./internal/core ./internal/store/sqlite ./internal/server
```

## Vydání

Vydání spouští podepsaný anotovaný tag `v*` (D43). Workflow `release` nejdřív ověří, že GitHub podpis tagu ověřil a tag podepsal e-mail z proměnné repozitáře `RELEASE_TAGGER_EMAIL`, že tag ukazuje na sestavovaný commit ve větvi `main` a že prostředí `release` vyžaduje schválení. Webového klienta sestaví samostatný job, který smí repozitář jen číst (D52). Teprve po schválení job vydání sestaví binárky a image, podepíše je a přidá provenance. Archivy obsahují `nylzo` pro Linux (amd64, arm64), macOS (arm64) a Windows (amd64, zip); image na ghcr.io obsahuje tutéž binárku a spouští se s příkazem `serve`. Před prvním vydáním proto v nastavení repozitáře založte prostředí `release` s povinným schválením a proměnnou `RELEASE_TAGGER_EMAIL` s e-mailem, kterým tagy podepisujete a který máte ověřený na svém účtu GitHub.

```bash
git tag -s v0.1.0 -m "nylzo 0.1.0"
git push origin v0.1.0
```

Build je reprodukovatelný (AK15): CI sestaví klienta i binárky na runneru x64 i arm64 a porovná hashe, a sestavení na macOS dává stejné bajty (ověřeno 24. 10. 2026, D64). Vydání ověříte sestavením stejného tagu s nástroji z `mise.lock`; úloha porovná `checksums.txt` s vydáním na GitHubu:

```bash
git checkout v0.1.0
```

```bash
mise run release:verify
```

Bez mise to samé udělá GoReleaser 2.18.3 se stejnými verzemi Go, Node.js a pnpm: `goreleaser release --clean --skip=publish,sign,sbom,ko,validate` a porovnání `dist/checksums.txt` bez řádků SBOM s `checksums.txt` vydání. Postup vydání pro maintainera včetně zveřejnění vlastního hashe je v `docs/release-checklist.md`.

Self-host přes Docker Compose popisuje `deploy/compose/README.md`.

## Licence

MIT, viz `LICENSE`.
