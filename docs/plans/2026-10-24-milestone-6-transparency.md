# Plán: milník 6, transparentnost a ověření klienta

Datum: 24. 10. 2026. Větev: `claude/transparency` z `main` po sloučení PR 9. Zdroj pravdy: `docs/designs/nylzo-architecture-research.md` (Next Steps 8, Z2, „Distribution Plan“, tabulka „Zveřejnit“), `spec/protocol.md` §9, §10 a §13, `docs/research/review-server-compromise.md` (nálezy 2, 5 a 12). Stav: vlastník 24. 10. 2026 napsal, ať přes noc samostatně udělám co nejvíc bodů specifikace v pořadí a sám merguji.

## Cíl milníku

Z bodu 8 Next Steps ta část, která nepotřebuje vlastníka ani právníka: stránka transparentnosti, SECURITY.md a manifest vloženého webového klienta, ke kterému patří `nylzo verify-client`. Podmínky služby, kontaktní místo podle DSA a právní posouzení zůstávají vlastníkovi; stránka pro ně má místo, které vyplní konfigurace instance.

Hotovo znamená: server servíruje manifest a údaje pro stránku transparentnosti, webový klient ukazuje stránku v češtině i angličtině, `nylzo verify-client` porovná, co server servíruje, se svou vloženou kopií, SECURITY.md popisuje hlášení, testy včetně e2e a mutační kontroly ověřovacích pravidel, otevřený PR.

## Rozhodnutí

### D60: manifest webového klienta

`GET /.well-known/nylzo/client.json` vrátí verzi serveru a pro každou stránku a soubor klienta cestu a SHA-256. Hash `index.html` je hash šablony se zástupným řetězcem nonce, protože server do každé odpovědi vkládá nový nonce; ověřovatel nonce z hlavičky CSP v těle nahradí zástupným řetězcem a pak porovná. U souborů v `/assets/` je to hash nekomprimované podoby; gzip varianta se musí rozbalit na stejné bajty, což server kontroluje při startu. Bez sestaveného klienta manifest řekne `"built": false` a žádné soubory.

Formát je inspirovaný WAICT, ale není to WAICT: jeho specifikace je v říjnu 2026 návrh a prohlížeče ho neověřují. Cesta leží pod `/.well-known/nylzo/`, aby nezabrala budoucí cestu WAICT. Manifest je tvrzení serveru, ne důkaz: napadený server pošle manifest, který sedí na podvržený klient. Smysl má jen ve srovnání s nezávislým zdrojem, s vydáním na GitHubu nebo s kopií ve vlastní binárce.

### D61: `nylzo verify-client` už v první verzi

Design ho řadil do druhé verze. Je to ale jediná kontrola Z2, kterou si uživatel spustí sám, a nezávislý monitor ji může pouštět každou hodinu z cronu; proto jde do první verze. Rozhodnuto samostatně v noci 24. 10. 2026 v rámci pokynu vlastníka, vlastník může vrátit.

- `nylzo verify-client --server URL` (nebo `NYLZO_SERVER`) zjistí z manifestu verzi serveru. Když se liší od verze binárky, skončí kódem 1 se zprávou, kterou verzi `nylzo` použít; jinak nemá s čím porovnat.
- Stáhne každou stránku (`/`, `/sent`, `/report`, `/transparency` a `/m/{náhodné id}`), ověří, že hlavička CSP je přesně ta ze spec §9 s jedním nonce, nahradí nonce zástupným řetězcem a porovná tělo s vloženou šablonou.
- Každou stránku i každý soubor z vlastní vložené kopie stáhne dvakrát: s `Accept-Encoding: identity` a s hodnotou, kterou posílají prohlížeče (`gzip, deflate, br, zstd`), protože útočník by mohl podvrhnout jen variantu pro prohlížeče. Odpověď v gzip rozbalí s limitem, jiné kódování bere jako neshodu, a obě podoby porovná s vloženými bajty.
- Ověří i hlavičky: `Content-Type`, CSP a bezpečnostní hlavičky ze spec §9 musí být přesně ty, které posílá tato verze. Server, který by klienta nechal beze změny a jen oslabil CSP, kontrolou neprojde.
- Porovná manifest serveru s vlastní kopií: stejné cesty, stejné hashe.
- Mluví se serverem stejně jako ostatní příkazy (jen HTTPS kromě loopback, bez přesměrování, omezená těla) a vypíše jen počet ověřených souborů, nebo první neshodu bez obsahu odpovědi.
- Co neumí, řekne README i výstup příkazu: cílený útočník pošle ověřovateli čistého klienta a oběti podvrženého (nález 2 z `review-server-compromise.md`). Kontrola odhalí plošnou záměnu, ne cílenou.

### D62: stránka transparentnosti

`/transparency` ve webovém klientovi, česky i anglicky. Obsah:

- Co server ukládá, jak dlouho a co to prozradí: zkrácená tabulka ze spec §13.
- Co loguje a co ne (spec §10), co vidí poskytovatel hostingu a TLS proxy (nález 12).
- Limity instance a verze serveru živě z `GET /.well-known/nylzo/transparency.json`, hashe souborů klienta z manifestu a příkaz `nylzo verify-client` pro vlastní kontrolu.
- Provozovatel, kontakt a hosting z konfigurace: `NYLZO_OPERATOR`, `NYLZO_CONTACT` (e-mail nebo `https://` adresa) a `NYLZO_HOSTING`. Když nejsou nastavené, stránka napíše, že je provozovatel neuvedl. Server hodnoty při startu ověří (délka nejvýš 200 znaků, žádné řídicí znaky, kontakt jen e-mail nebo `https://`), stránka je vypisuje jako text a odkaz dělá jen z ověřeného kontaktu.

`GET /.well-known/nylzo/transparency.json` vrací verzi serveru, provozovatele (jen nastavená pole), limity zprávy, limity vytváření a čtení, obtížnost PoW a doby uchování, které server opravdu používá (náhrobky 7 dní, hlášení zneužití 7 dní, denní čítače 24 hodin po konci okna). Hodnoty bere přímo z konfigurace a konstant kódu, takže stránka nemůže tvrdit něco jiného, než server dělá. Na rozdíl od `/api/v1/instance` nepotřebuje databázi a nevydává id. Obě cesty pod `/.well-known/nylzo/` dostávají stejné bezpečnostní hlavičky jako API a neprocházejí token bucketem, protože nesahají do databáze, stejně jako soubory klienta.

### SECURITY.md

Anglicky, protože ji čtou bezpečnostní výzkumníci odkudkoli. Hlášení přes soukromé hlášení zranitelností na GitHubu; to musí vlastník v nastavení repozitáře zapnout (24. 10. 2026 vypnuté). Co patří do rozsahu (protokol, obě jádra, server, CLI, webový klient, příklad Compose), co ne (známé hranice ze spec §13), co do hlášení napsat. Lhůty odpovědi a safe harbor jsou slib vlastníka, proto je SECURITY.md nedává.

## Mimo milník

- Podmínky služby, kontaktní místo a oznámení podle DSA, GDPR posouzení: vlastník a právník.
- Hashe klienta v release notes a nezávislý monitor u jiného poskytovatele: monitor potřebuje veřejnou instanci; release notes v dalším kroku.
