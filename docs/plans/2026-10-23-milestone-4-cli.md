# Plán: milník 4, CLI a binárka „vše v jedné“

Datum: 23. 10. 2026. Větev: `claude/cli` z `main` po sloučení PR 7 (`01de35e`). Zdroj pravdy: `docs/designs/nylzo-architecture-research.md` (sekce „CLI, MCP a budoucí skill“, „Kde tajemství může zůstat“, AK14, „Distribution Plan“) a `spec/protocol.md` §2 až §6 a §8.1. Stav: vlastník 23. 10. 2026 řekl, že by se mu líbila jedna binárka se serverem, CLI i webem, a požádal o samostatnou práci bez otázek, pokud nejde o rozhodnutí, které musí udělat on.

## Cíl milníku

Příkazová řádka `nylzo` pro vytvoření, přečtení, stav, zrušení a smazání zprávy (bod 6 v Next Steps, rozsah první verze v „Rozdělení na MVP“). Stejná binárka obsahuje i server s vloženým webovým klientem (`nylzo serve`), takže kdo si ji stáhne, má celé Nylzo v jednom souboru, a stejná binárka běží i v produkci na jednom hostingu (D56, D57).

Hotovo znamená: AK14 prochází, CLI vytvoří zprávu, kterou přečte webový klient, a přečte zprávu z webového klienta (e2e), jednotkové a integrační testy proti skutečnému serveru, mutační kontrola bezpečnostních pravidel, vydání sestaví `nylzo` pro Linux, macOS a Windows a otevřený PR proti `main`.

## Rozhodnutí

### D53: jedna binárka `nylzo`

Jediná binárka `nylzo` obsahuje CLI (`create`, `read`, `status`, `cancel`, `burn`) i server (`serve`, `sweep-once`) s vloženým webovým klientem. Bez příkazu vypíše nápovědu. `nylzod` zaniká: image na ghcr.io spouští `nylzo` a Compose i hosting mu předají příkaz `serve`. Archivy vydání obsahují jen `nylzo`. Rozhodnuto podle přání vlastníka 23. 10. 2026 („jen jedna binárka all-in-one, aby nebyl server a front-end zvlášť“).

Cena: kdo chce jen posílat zprávy, stahuje i server a SQLite (kolem 12 MB místo menšího klienta), a oprava jedné části vydá novou verzi obou. Bezpečnost to nemění: CLI chrání před napadeným serverem tím, že jeho kód pochází z podepsaného vydání (Z2), ne tím, že je v oddělené binárce.

### D56: jeden hosting, web na stejném originu

Webový klient zůstává vložený v binárce i na veřejné instanci; samostatný statický origin (OD23, D31) vlastník 23. 10. 2026 zrušil. Cena: kdo ovládne server nebo hosting, podvrhne i klienta a oddělený origin už útok neztíží (Z2).

### D57: hosting prototypu

Vlastník chce prototyp v produkci co nejjednodušeji, požadavek na provoz jen v EU zatím odkládá a TLS v produkci ukončí hosting (v Compose proxy před `nylzo`). Rešerše `docs/research/I-hosting-prototype.md` (23. 10. 2026) potvrdila, že stačí jeden kontejner s trvalým POSIX svazkem, ale serverless platformy ho většinou nemají nebo mají krátké limity: Vercel nevyhovuje (tělo nejvýš 4,5 MB, žádný trvalý disk, funkce nejvýš 300 s na Hobby), stejně tak Cloud Run, Scaleway Serverless Containers a DigitalOcean App Platform (bez svazku), Railway (nahrání do 5 minut) a Koyeb (100 s).

- Doporučení: malý VPS s hotovým `deploy/compose` (Caddy a `nylzo serve`), například DigitalOcean Droplet 1 GiB ve Frankfurtu za 6 USD měsíčně. Server nepotřebuje změnu, TLS končí na vlastním stroji a access logy platformy nevznikají; cenou je správa stroje.
- Druhá volba: Fly.io (jeden Machine 256 MB a 5GB volume, odhadem 3,3 USD měsíčně). Potřebovala by nový režim proxy pro hlavičku `Fly-Client-IP`, test, zda ji proxy přepíše, a odpověď Fly, co loguje.
- Na vlastníkovi: volba platformy, účet s kartou, doména a DNS záznamy, způsob nasazení. Nasazení samo potřebuje účet vlastníka; Nylzo účty nezakládá.

### D54: server jen na Linuxu a macOS

`nylzo serve` a `nylzo sweep-once` na Windows skončí s chybou, která doporučí WSL nebo Docker. Úložiště potřebuje fsync adresáře a server kontrolu volného místa (D48); Windows nemá ani jedno a server bez nich by tiše slevil ze záruk. CLI na Windows funguje celé.

### D55: kudy CLI bere a kam dává tajemství

- Žádný argument ani přepínač nenese tajemství, odkaz s fragmentem, passphrase ani M (CWE-214, AK14). Příkazy CLI nepřijímají poziční argumenty vůbec a argument, který vypadá jako odkaz s klíčem, odmítnou se zvláštní hláškou, ještě než pošlou jakýkoli požadavek.
- `create` čte text ze stdin, nebo přílohu z `--from-file`. Passphrase z `--passphrase-file`, z `NYLZO_PASSPHRASE_FILE`, nebo s `--ask-passphrase` dvakrát z terminálu bez echa. Server zadá `--server` nebo `NYLZO_SERVER`. Na stdout vypíše jen odkaz.
- Správcovský klíč M (`status`, `cancel`) zapíše `create` jen na výslovné přání: `--manage-token-file` vytvoří nový soubor s právy 0600, `--print-manage-token` ho vypíše za odkaz. Správcovský záznam má dva řádky, adresu zprávy bez fragmentu a M; neobsahuje `S`, takže zprávu přečíst neumí.
- `read` a `burn` čtou odkaz ze stdin, z `--link-file` nebo z `NYLZO_LINK`. `status` a `cancel` čtou správcovský záznam ze stdin, z `--manage-token-file` nebo z `NYLZO_MANAGE_TOKEN_FILE`.
- `read` založí nový soukromý adresář (0700, soubory 0600, `O_EXCL`) v dočasném adresáři nebo uvnitř `--out` ještě před claimem, zapíše do něj text a přílohu a vypíše cesty. Text na stdout jen s výslovným `--stdout` a bez řídicích znaků, když jde do terminálu. Název přílohy CLI očistí stejně jako web (sdílená tabulka případů v testech) a nikdy nezapíše mimo cílový adresář. Na Windows práva 0700/0600 přístup neomezují.
- Spojení jen přes HTTPS, s výjimkou přesně `localhost` a adres loopback, stejně jako prohlížeč povoluje Web Crypto jen v bezpečném kontextu; u nich CLI ověří, že spojení opravdu míří na loopback, a nepoužije proxy. Přesměrování CLI odmítne, cookies neukládá. Chybové hlášky neopakují hodnoty přepínačů ani cesty.
- S, R, salt_pw, P, V a M CLI i jádro (HKDF, Ed25519) po použití přepíšou nulami. Passphrase jako řetězec přepsat nejde a Go může při úklidu paměti nechat kopie, proto je to snaha, ne záruka; CLI nikdy nezapisuje tajemství do logu ani do chybové hlášky.

## Chování

- `create`: zjistí limity a id z `GET /api/v1/instance`, ověří vstup proti limitům, odvodí klíče, zapečetí obálku, při požadovaném PoW si vezme novou výzvu až po odvození a vyřeší ji s rozpočtem 110 s ve dvou kolech (spec §8.1), pošle `POST /api/v1/messages`. Při 403 `id_token`, `pow` nebo 409 začne znovu s novým id, nejvýš třikrát (jako web). Slabou passphrase odmítne (12 znaků, spec), krátkou nebo málo slovnou ohlásí varováním.
- `read`: rozebere odkaz (spec §4), z hostitele odkazu určí server, načte metadata, ověří profil KDF (AK24), odvodí P a V jen u zprávy s passphrase, pošle claim, ověří politiku z odpovědi proti metadatům, otevře obálku a potvrdí ack. Když se stahování přeruší po hlavičce lease, zkusí v témže běhu `/blob` s tímtéž lease, nejvýš tolikrát, kolik lease dovolí.
- `status`, `cancel`, `burn`: podepsané požadavky podle spec §5.5 a §6.6. Každý podepsaný požadavek bere novou výzvu a po 404 jednou zkusí znovu (spec §5.5).
- Chyby serveru CLI přeloží na krátké hlášky bez tajemství (vypršelo, otevřeno, zrušeno, špatná passphrase s počtem zbývajících pokusů a čekáním, limit s `Retry-After`). Návratové kódy: 0 úspěch, 1 chyba, 2 špatné použití včetně odmítnutého tajemství v argumentu.

## Struktura

- `internal/client`: HTTP klient nad `internal/core` (instance, výzva, vytvoření, metadata, claim, blob, ack, burn, cancel, status), sdílený s budoucím MCP serverem.
- `internal/cli`: příkazy a jejich vstupy a výstupy včetně serverových, přesunutých z `cmd/nylzod`.
- `cmd/nylzo`: tenký vstup; `cmd/nylzod` zaniká.
- Testy: klient i CLI proti skutečnému serveru (`server.New` nad SQLite v `httptest`), AK14 s kontrolou `ps` během běžícího příkazu, kontrola, že žádný přepínač nenese tajemství, e2e mezi CLI a webovým klientem oběma směry.
- Vydání: build `nylzo` pro linux/amd64, linux/arm64, darwin/arm64 a windows/amd64 (zip) podle „Distribution Plan“, image z téhož `cmd/nylzo`, Compose s příkazem `serve`.

## Mimo milník

`verify-client`, MCP server a skill (druhá verze), hlášení zneužití z CLI, podpis binárek pro macOS a Windows (placené účty, rozhodne vlastník), oddělený origin webového klienta (OD23).
