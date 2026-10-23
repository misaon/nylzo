# Plán: milník 3, webový klient po PoC

Datum: 23. 10. 2026. Větev: `claude/web-after-poc` z `main` po sloučení PR 5 (`3244bae`). Zdroj pravdy: `docs/designs/nylzo-architecture-research.md` (sekce „Webový klient: srovnání a volba (D12)“, „Hlavičky a chování klienta“, „Ochrana proti zneužití“) a `spec/protocol.md` §6.6 a §9. Stav: vlastník schválil D39 až D41 podle doporučení 23. 10. 2026.

## Cíl milníku

Doplnit funkce, které plán PoC odložil (D29): stránku `/sent` se stavem a zrušením přes M, smazání příjemcem (burn), hlášení zneužití z UI a přepínač jazyka. Hlášení zneužití je podle DSA nutné před veřejným nasazením. Odhad délky scryptu benchmarkem a vizuální návrh do milníku nepatří, vzhled dělá vlastník.

Hotovo znamená: unit testy logiky, e2e testy nových obrazovek v Chromiu, Firefoxu a WebKitu s CSP v režimu enforce, AK18 až AK20 i na nových obrazovkách, mutační kontrola bezpečnostních pravidel a otevřený PR proti `main`.

## Rozsah

- `/sent`: seznam zpráv, které si toto zařízení zapamatovalo. Klient načte stav každé zprávy (`GET …/status` podepsaný M) postupně, ne všechny naráz, a ukáže stav, použitá otevření z maxima, platnost, první otevření, konec a počet chybných pokusů o passphrase (D19). Zrušení s potvrzovacím krokem (`POST …/cancel`). Záznam jde zapomenout, stejně tak všechny naráz. Zpráva, kterou server už nezná (404), se ukáže jako neexistující s nabídkou ji zapomenout. Odkaz na zprávu stránka neukazuje, protože `S` se nikam neukládá.
- Vytvoření: zaškrtávátko „Zapamatovat na tomto zařízení pro stav a zrušení“, ve výchozím stavu zapnuté. Po vytvoření klient uloží záznam a výsledek odkáže na `/sent`. Když uložení selže (IndexedDB chybí nebo ji prohlížeč v anonymním režimu odmítne), výsledek to řekne a odkaz platí dál.
- Čtení: tlačítko Smazat s potvrzovacím krokem před zobrazením a po zobrazení, dokud zbývají otevření (`POST …/burn`). Po smazání stránka ukáže stav „smazáno“.
- Hlášení zneužití: formulář na stránce čtení, dokud stránka drží klíč R, a stránka `/report`, kam se vloží odkaz. Klient odkaz rozebere podle spec §4, odvodí R a pošle `POST /api/v1/abuse`; hlášení nikdy nespotřebuje otevření. Důvod je volitelný, nejvýš 1000 bajtů UTF-8, bez kontroly pravopisu a automatických oprav (stejný důvod jako D38). Server odpovídá 202 pro každé id stejně, proto stránka vždy řekne jen, že hlášení přijala. Text vysvětlí, že hlášení posoudí člověk a že se nic nemaže automaticky.
- Přepínač jazyka: čeština a angličtina v hlavičce stránky. Jazyk se přepne bez nového načtení, protože stránka čtení by po něm ztratila zobrazený obsah. Volba se uloží do `localStorage` pod `nylzo:lang` (není citlivá) a určuje i `lang` elementu `html`. Bez uložené volby platí `navigator.languages` jako dosud.
- Navigace: odkazy Nová zpráva, Odeslané a Nahlásit zneužití v hlavičce. Přechod je plné načtení stránky, takže každá stránka dostane nový nonce.
- Server: `GET /sent` a `GET /report` servírují stejnou stránku jako `/` (spec §9).

## Rozhodnutí vlastníka

### D39: kde odesílatel drží M

- A. IndexedDB, M jen jako neexportovatelný `CryptoKey` Ed25519. Záznam drží id, čas vytvoření, expiraci a klíč. Smaže se 7 dní po expiraci (po té době už server nezná ani náhrobek) při každém načtení aplikace. Ruční zapomenutí jednoho záznamu i všech.
- B. `localStorage` s bajty M, jak dosud psal návrh. Jednodušší, ale M v něm leží čitelně pro každý skript na stránce, malware na disku i zálohu profilu.
- C. Nic neukládat a dát odesílateli správcovský odkaz s M. Funguje napříč zařízeními, ale M pak žije ve schránce a v poznámkách jako další tajný odkaz.

Doporučení: A. Klíč jde na stránce použít k podpisu, ale jeho bajty nikdo nepřečte, ani skript na stránce. Rozhodnuto: A, ve formuláři vytvoření ve výchozím stavu zapnuté, protože zrušení je pojistka odesílatele pro odkaz poslaný špatnému člověku.

### D40: smazání příjemcem a klíč R po zobrazení

- A. Smazat před zobrazením i po něm, dokud zbývají otevření. Po zobrazení stránka do zavření drží jen neexportovatelný klíč R, který umí podepisovat, ale ne dešifrovat. `S`, `salt_pw`, `K` a `V` zahazuje hned jako dosud (SEC-8).
- B. Smazat jen před zobrazením. Po zobrazení stránka nic nedrží a příjemce z ní nesmaže zbývající otevření ani nepošle hlášení.

Doporučení: A. Klíč R po zobrazení přidá jen to, co už umí tlačítko Smazat: s ním jde zprávu smazat, nahlásit nebo spotřebovat zbývající otevření, ale obsah z nich bez `S` nikdo nedešifruje a zpráva s passphrase k otevření potřebuje i `V`. Rozhodnuto: A, pravidlo SEC-8 v návrhu se upraví.

### D41: odkud jde hlášení zneužití

- A. Formulář na stránce čtení, dokud stránka drží R, a stránka `/report` s vloženým odkazem pro toho, kdo stránku už nemá.
- B. Jen `/report`.
- C. Jen stránka čtení.

Doporučení: A. Návrh chce odkaz na hlášení na každé stránce čtení a `/report` pokryje ukončené zprávy a zavřené stránky. Rozhodnuto: A.

## Bezpečnost

- M existuje jako bajty jen během vytvoření: `requestKey` z něj udělá neexportovatelný privátní klíč a seed se hned vynuluje. IndexedDB uloží `CryptoKey` strukturovaným klonem, takže bajty nikdy nevzniknou znovu. E2e test ověří v každém prohlížeči, že uložený klíč má `extractable = false` a `exportKey` selže.
- Kdo má přístup k zařízení, vidí v IndexedDB id zpráv a časy jejich vytvoření a expirace a s klíčem může zprávu zrušit nebo číst její stav. Obsah ani odkaz tam nejsou. Zaškrtávátko na formuláři to vypíná.
- Klíč R po zobrazení je neexportovatelný a zmizí se stránkou. Formulář hlášení podepisuje tělo požadavku (spec §5.5), takže zaznamenané hlášení nejde poslat s jiným důvodem ani k jinému id.
- `/report` vložený odkaz nikam neposílá: z odkazu klient odvodí R a pošle jen id, důvod a podpis. Pole s odkazem má vypnutou kontrolu pravopisu, automatické opravy i nápovědu prohlížeče.
- CSP se nemění; IndexedDB ani `localStorage` CSP neomezuje.

## Struktura

- `web/app/src/lib/sent.ts`: rozhraní úložiště záznamů, implementace nad IndexedDB, retence, tok stránky `/sent` jako stavový automat bez DOM.
- `web/app/src/lib/report.ts`: sestavení a odeslání hlášení z odkazu nebo z klíče R.
- `web/app/src/lib/read.ts`: burn a hlášení, klíč R po zobrazení.
- `web/app/src/lib/i18n.svelte.ts`: slovník s reaktivním jazykem a uložením volby.
- `web/app/src/screens/Sent.svelte`, `Report.svelte`, úpravy `Create.svelte`, `Read.svelte` a `App.svelte`.
- `internal/server`: cesty `/sent` a `/report`.

## Kroky

1. Plán a rozhodnutí (tento dokument).
2. Spec §9 a návrh: cesty stránek, D39 až D41, obrazovky, úprava SEC-8, retence záznamů, přepínač jazyka.
3. Server: cesty a testy v Go.
4. Klient: logika v TypeScriptu s unit testy, potom obrazovky.
5. E2e testy, AK18 až AK20 na nových obrazovkách, mutační kontrola.
6. PR proti `main`.

## Testy

- Unit: retence a řazení záznamů, tok `/sent` (načtení stavů postupně, zrušení, zapomenutí, 404), burn a hlášení v toku čtení, tělo hlášení a podpis přes tělo, limit délky důvodu, přepínač jazyka s nedostupným `localStorage`.
- E2e: zapamatovaná zpráva na `/sent` a její zrušení, které příjemce uvidí; vypnuté zaškrtávátko nechá `/sent` prázdné; neexportovatelný klíč v IndexedDB; smazání před zobrazením a po něm; hlášení ze stránky čtení i z `/report`; přepnutí jazyka bez nového načtení stránky; CLS, pohyb, axe a dotykové cíle na nových obrazovkách.
- Go: `/sent` a `/report` dostanou stránku s nonce a celou CSP podle spec §9.
