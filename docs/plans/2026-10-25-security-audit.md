# Plán: opravy podle nezávislého bezpečnostního auditu

Datum: 25. 10. 2026. Větev: `fix/security-audit` z `main` (545b379). Stav: vlastník 25. 10. 2026 předal zprávu nezávislého bezpečnostního experta a požádal o důkladné posouzení a opravu nebo vylepšení všech nedostatků. Platí stálé pravidlo, že bezpečnostní slabinu nejdřív opravíme v návrhu a specifikaci a potom v kódu a žádnou neoznačíme jako přijaté riziko.

## Zpráva a její ověření

Expert auditoval implementaci bez projektových dokumentů. Každý z deseti nálezů jsme ověřili v kódu na `545b379` a všechny odpovídají skutečnosti. Pozitivní část zprávy (správné složení AES-GCM a HKDF, žádné opakování IV, žádné obejití autorizace, transakční claim a lease) potvrzuje dosavadní testy.

| Nález zprávy | Závažnost podle experta | Ověření | Řešení |
|---|---|---|---|
| Převzetí serveru umožní podstrčit upravený webový klient | zásadní | hranice návrhu, Z2 to už uvádí | D100, otevřené OD27 |
| CLI nechává dešifrované zprávy na disku | vysoká | potvrzeno, `read` bez přepínače zapisuje do dočasného adresáře | D92 |
| Nedůsledné mazání citlivé paměti | střední | potvrzeno pro Go `Seal`/`Open`, passphrase a přílohu v composeru | D94 |
| Slabé passphrase jdou při úniku odkazu hádat offline | vysoká při úniku odkazu | potvrzeno, `aaaaaaaaaaaa` projde, slabost jen varuje | D93 |
| Expirace a počet otevření nejsou kryptografické zničení | zásadní hranice návrhu | hranice návrhu, Z4 a Z7 to říkají jen zčásti | D100 |
| Fragment zůstává v adresním řádku až do zobrazení | střední | potvrzeno | D95 |
| Uložené správcovské klíče jsou dostupné skriptu stejného originu | střední | potvrzeno, zapamatování je ve výchozím stavu zapnuté | D96 |
| Režim Scaleway věří `X-Real-IP` od kohokoli | střední, podle nasazení | potvrzeno při prázdném `NYLZO_TRUSTED_PROXIES` | D97 |
| Výchozí listener je veřejné nešifrované HTTP | střední, podle nasazení | potvrzeno, `:8080` na všech rozhraních | D98 |
| Čitelná metadata a nekontrolovaná existující oprávnění | střední pro soukromí | potvrzeno, `0600`/`0700` platí jen při vytvoření | D99 |

## Rozhodnutí

D92: `nylzo read` nikdy nezapisuje na disk bez výslovného přání.
- Příkaz vyžaduje `--stdout`, `--out DIR`, nebo obojí; bez nich skončí chybou použití dřív, než pošle jakýkoli požadavek. Výchozí dočasný adresář zaniká.
- `--stdout` vypíše text na stdout a na disk nic nezapíše. Délka objektu z metadat ukáže, jestli zpráva může nést přílohu (spec §3, D16); když ano a chybí `--out`, příkaz skončí ještě před claimem a otevření nespotřebuje. Do terminálu vypíše řídicí znaky viditelně escapované a na stderr řekne proč; do roury nebo souboru jde text bajt po bajtu.
- `--out DIR` funguje jako dosud (nový adresář 0700, soubory 0600, `O_EXCL`). Po zápisu CLI připomene, že soubory drží plaintext a že je má uživatel smazat; na SSD a v zálohách smazání nemusí stačit.
- Agentní skill dál používá `--out` do soukromého adresáře, aby se plaintext nedostal do kontextu modelu.
- Nahrazuje část D55 o výchozím zápisu do dočasného adresáře.

D93: passphrase podle NIST SP 800-63B (§3.1.1.2) a generátor.
- Klient MUST odmítnout passphrase, která po NFC nesplní jedno z pravidel: aspoň 12 kódových bodů; aspoň 6 různých znaků po převodu ASCII písmen na malá; nesmí opakovat kratší úsek; bez oddělovačů (mezera, `-`, `_`, `.`) nesmí být souvislým úsekem abecedy, číslic nebo řady klávesnice QWERTY či QWERTZ, dopředu ani pozpátku; po odstranění slova `nylzo` musí zůstat aspoň 12 znaků; nesmí být v seznamu běžných hesel. Seznam je 1188 hesel o aspoň 12 znacích ze seznamu NCSC 100k (UK National Cyber Security Centre, Open Government Licence v3.0, převzato z SecLists), malými písmeny. Obě jádra mají stejnou kontrolu a vektory ji hlídají včetně SHA-256 seznamu.
- Obě jádra umí vygenerovat passphrase: 16 znaků z abecedy Crockford base32 po čtyřech oddělených pomlčkou (80 bitů), z CSPRNG bez zkreslení; vygenerovanou kontrola vždy přijme. Web ji nabídne tlačítkem v nabídce zabezpečení, CLI přepínačem `--generate-passphrase-file FILE` (nový soubor 0600, passphrase nikdy na stdout).
- Doporučení SHOULD (aspoň 16 znaků a 4 slova) počítá za oddělovače slov i pomlčku, takže vygenerovaná passphrase varování nevyvolá.
- Parametry scryptu (N=2^17, r=8, p=1) zůstávají, odpovídají minimu OWASP a iPhone je zvládne.

D94: přepisování citlivých bajtů v paměti tam, kde to jazyk dovolí.
- Go: `Seal` přepíše doplněný plaintext po zašifrování, `Open` přepíše části dešifrovaného bufferu, které nevrací, `DerivePassphraseKey` přepíše normalizovanou passphrase po scryptu, CLI přepíše přečtenou passphrase a obsah po zápisu.
- Web: composer přepíše bajty přílohy po vytvoření i po chybě; čtenář už přepisuje text, přílohu a klíče při odchodu.
- Hranice zůstává a dokumenty ji říkají: řetězce v Go i JavaScriptu jsou neměnné, garbage collector může kopie přesunout a operační systém stránky odložit na disk. Proti napadenému procesu přepisování nepomůže.

D95: čtenář odstraní fragment z adresního řádku hned po načtení do paměti, ještě před prvním požadavkem, a to i u neplatného odkazu. Do `sessionStorage` si uloží značku bez tajemství, aby po obnovení stránky před otevřením vysvětlil, že klíč odstranil kvůli bezpečnosti a odkaz je potřeba otevřít znovu. Jen po dobu lease, tedy od claimu do zobrazení nebo konce lease, drží v `sessionStorage` karty i fragment, aby obnovení stránky dokončilo otevření, které už spotřebovalo, bez ztráty zprávy; po zobrazení nebo konci lease ho smaže. To je kratší a méně viditelné než dosavadní fragment v adrese až do zobrazení. Globální historie a synchronizace prohlížeče mohou původní adresu zachytit už při navigaci; to replaceState nezmění a dokumenty to dál říkají.

D96: „Zapamatovat na tomto zařízení“ je ve výchozím stavu vypnuté a odesílatel ho zapne sám. Nahrazuje výchozí stav z D39. Uložený neexportovatelný klíč podepíše stav a zrušení každému skriptu stejného originu, obsah ani odkaz neprozradí.

D97: `NYLZO_PROXY_MODE=scaleway` bez `NYLZO_TRUSTED_PROXIES` čte `X-Real-IP` jen ze spojení z adres loopback, link-local, privátních (RFC 1918, ULA) a sdíleného prostoru CGNAT (100.64.0.0/10). Spojení z veřejné adresy, které obešlo ingress, si kyblík nevybere. Pokud test R13 ukáže, že ingress Scaleway přichází z veřejné adresy, provozovatel nastaví `NYLZO_TRUSTED_PROXIES`; do té doby server varuje `forwarded-header-ignored` a všichni sdílí kyblík ingressu (bezpečné pro rozpočet, horší pro dostupnost).

D98: server ve výchozím stavu naslouchá jen na `127.0.0.1:8080`. Jiné rozhraní se povolí výslovně přes `NYLZO_LISTEN`; proměnná `PORT` (konvence hostingů) dál znamená všechna rozhraní. Když server naslouchá jinde než na loopbacku, zapíše při startu varování, že mluví nešifrované HTTP a musí stát za proxy s TLS. Compose příklad nastavuje `NYLZO_LISTEN: ":8080"` uvnitř své sítě už dnes.

D99: kontrola oprávnění při startu.
- Datový adresář, databáze s `-wal` a `-shm`, adresář blobů a adresář hlášení musí patřit uživateli procesu a nesmí dávat práva skupině ani ostatním. Patří-li uživateli procesu a práva jsou širší, server je zúží na 0700 a 0600 a zapíše varování; patří-li jinému uživateli, server odmítne start.
- Soubor s pepperem (`NYLZO_PEPPER_FILE`) nesmí číst skupina ani ostatní; jinak server odmítne start, podobně jako ssh u soukromého klíče.
- Metadata (časy po minutách, doplněné velikosti, počty otevření, stav) server pro vynucení pravidel potřebuje; spec §13 je vyjmenovává a retence zůstává nejkratší, jaká pravidla dovolí (náhrobek 7 dní).

D100: hranice návrhu řečené přímo.
- Z2: proti serveru nebo build pipeline, které podvrhnou webový klient, chrání jen klient z nezávislého a ověřeného zdroje: podepsané CLI dnes, místní ověřený webový klient podle OD27 později. Stránka transparentnosti a README to říkají uživateli.
- Z4, Z5 a Z7: expirace a počet otevření jsou pravidla, která vynucuje server, ne kryptografické zničení. Napadený server může ciphertext ponechat a stav ignorovat; obnovená starší databáze vrátí spotřebovaná otevření. Ochranu pak dává jen utajení odkazu a passphrase poslaná jiným kanálem. Provozovatel nesmí obnovovat zálohu databáze do běžící instance a Nylzo zálohy obsahu ve výchozím stavu nedělá (OD20).

OD27 (otevřené, pro vlastníka): místní ověřený webový klient. Podepsaná binárka `nylzo` už nese webový klient; příkaz by ho servíroval na loopbacku a požadavky API předával instanci, která by dál viděla jen ciphertext a podpisy. Návrh doporučuje udělat, protože převádí Z2 z „neplatí“ na „platí pro uživatele místního klienta“. Vyžaduje rozhodnout UX (jak odkaz z instance otevřít v místním klientovi bez tajemství v argumentu příkazu, adresa odkazů vytvořených místně) a je to samostatný milník.

## Postup

1. Návrh, specifikace a tento plán.
2. Jádra Go a TS: pravidla passphrase, generátor, seznam běžných hesel, `MayHoldFile`, přepisování bajtů; vektory.
3. CLI: `read` s výslovným cílem, `--generate-passphrase-file`, skill a README.
4. Server: výchozí listener, režim Scaleway, kontrola oprávnění.
5. Web: tlačítko generátoru, odmítnutí slabé passphrase, fragment hned pryč, zapamatování vypnuté, přepis přílohy, texty transparentnosti.
6. `mise run lint`, Go testy s `-race`, testy obou jader a aplikace, e2e v Chromiu a WebKitu.
