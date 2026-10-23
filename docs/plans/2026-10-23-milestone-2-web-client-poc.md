# Plán: milník 2, webový klient (proof of concept)

Datum: 23. 10. 2026. Větev: `claude/focused-kirch-80a68e` z `main` po sloučení PR 1 (`9cb63b3`). Zdroj pravdy: `docs/designs/nylzo-architecture-research.md` (sekce „Webový klient: srovnání a volba (D12)“, „Vizuální design a interakce“, „Zkouška OD6“, Next Steps 5) a `spec/protocol.md` §2 až §9. Stav: schváleno vlastníkem 23. 10. 2026 s volbami D19 C, D28 A a D29 podle doporučení; kroky 1 až 5 běží, test OD6 na iPhonu pouští vlastník.

## Cíl milníku

Minimální webový klient nad hotovým serverem `nylzod` a jádrem `@nylzo/core`. Odesílatel vytvoří zprávu s textem nebo souborem do 5 MB, nastaví platnost a počet otevření, volitelně passphrase, a dostane odkaz. Příjemce odkaz otevře, po kliku zprávu zobrazí nebo stáhne přílohu. Klienta servíruje `nylzod` z vložených souborů (D27); oddělený origin (OD23) do PoC nepatří a musí být hotový před veřejným nasazením.

Hotovo znamená: e2e testy v Chromiu, Firefoxu a WebKitu s CSP v režimu enforce, zelená kritéria AK2, AK3, AK10, AK13, AK16, AK18 až AK20 a e2e část AK24 v CI, výsledek OD6 z iPhonu v návrhu a otevřený PR proti `main`.

## Rozsah

V PoC:

- `/` vytvoření: text, soubor, platnost (1 h, 24 h, 3 d, 7 d podle OD4, jen do maxima instance), počet otevření (1 až maximum instance), passphrase volitelně (nejméně 12 znaků, varování pod 16 znaků nebo 4 slovy), profil KDF (výchozí z `/api/v1/instance`, volba profilu 1 nebo 2), PoW, když ho instance vyžaduje. Výsledkem je odkaz ke zkopírování.
- `/m/{id}` čtení: metadata podepsaná R (stav, platnost, zbývající otevření, passphrase ano či ne), tlačítko Zobrazit, zadání passphrase se skutečným průběhem odvození z `onProgress`, claim, dešifrování, ack, odstranění fragmentu z aktuálního záznamu historie, text nebo stažení přílohy. Lease v sessionStorage do dešifrování, aby reload během stahování zprávu nespálil (KD5). Chybové stavy podle návrhu: neznámý odkaz, vypršelo, otevřeno v čase T, zrušeno, smazáno, špatná passphrase s `retry_after` a `attempts_left`, zamčená zpráva, zařízení nezvládne KDF, poškozená obálka nebo politika, limit a nedostupnost serveru.
- PoW v klientovi podle spec §8.1.
- Servírování sestaveného klienta z `nylzod`.
- Slovník cs a en, jazyk podle `navigator.language`.

Mimo PoC (D29): stránka `/sent` se stavem a zrušením přes M, smazání příjemcem (burn), hlášení zneužití z UI (DSA, nutné před veřejným nasazením), odhad délky scryptu benchmarkem při načtení, přepínač jazyka, vizuální návrh. M klient vygeneruje kvůli `manage_key` (spec §6.2) a v PoC ho neukládá, takže zprávu z PoC nejde zrušit.

## Dělba práce s vlastníkem

Vizuální návrh a interakce (D13) dělá vlastník. Plán proto rozhoduje jen o struktuře a logice:

- Logika je v TypeScriptu mimo komponenty: HTTP klient, tok vytvoření a tok čtení jako stavové automaty se stavy a událostmi, PoW, KDF, stahování. Testuje se bez DOM.
- Komponenty Svelte jsou sémantická kostra bez vzhledu: formulář, tlačítka, stavy, `aria` atributy a správa fokusu, žádné barvy, písma, rozestupy, sklo ani animace. Tailwind 4 je zapojený s prázdným `@theme`. Vlastník komponenty přestyluje nebo přepíše; drží jen rozhraní stavových automatů.
- Kritéria AK18 až AK20 jsou v CI jako brány. Teď je splní kostra, později je musí splnit vlastníkův návrh.

## Rozhodnutí pro vlastníka

### D19: chybné pokusy o passphrase od držitele uniklého odkazu

Kdo má odkaz bez passphrase, vyvolá čekání pro všechny čtenáře. Podle spec §6.4 se po 6. chybném pokusu čeká 2 s, po každém dalším dvakrát déle, vždy zaokrouhleno nahoru na celou minutu. Po 17. chybném pokusu, ke kterému se útočník dostane zhruba za 70 až 80 minut, je každý další pokus až za hodinu a útočník může vystřelit hned, jak čekání skončí. Pravý příjemce se tak prakticky k pokusu nedostane. Po 100 chybných pokusech, tedy zhruba po 3,5 dne, je zpráva zamčená do konce platnosti. Obsah ani passphrase útočník nezíská.

- A. Reset čítače klíčem M: odesílatel podepíše nový endpoint a čítač i čekání se vynulují. Útočník čekání obnoví během minut a s každým resetem dostane dalších až 100 pokusů o passphrase. Dostupnost to nevrátí a online hádání to rozšíří.
- B. PoW za každý pokus o passphrase: útočníka to stojí sekundy na pokus, k hodinovému čekání mu stačí 17 pokusů, takže dostupnost to také nevrátí. Platí to i pravý příjemce na telefonu.
- C. Bez resetu. Protokol se nemění, dostupnost obnoví odesílatel zrušením a novým odkazem jiným kanálem. Klient příjemci zobrazí zamčený stav a radí požádat o nový odkaz; odesílatel uvidí počet chybných pokusů ve stavu, až přijde `/sent`. Návrh a spec zapíšou, že jde o ztrátu dostupnosti, kterou A ani B neodstraní.

Doporučení: C. A i B mění protokol a útok neodstraní, A navíc oslabuje strop 100 pokusů, který chrání passphrase. Rozhodnuto: C.

### D28: kde běží scrypt a PoW

Zkouška OD6 na macOS ukázala, že pět odvození na hlavním vlákně drží přes 600 MiB déle než minutu, zatímco worker ukončený po každém odvození drží špičku jednoho odvození (215 MiB proti 653 MiB).

- A. Module Worker pro scrypt profilu 1 i pro PoW, nový pro každé odvození a po něm ukončený. Vyžaduje `worker-src 'self'` v HTML CSP (spec §9, server, návrh; nejdřív dokumenty). Worker dostává od serveru CSP `default-src 'none'`, proto musí být jeden soubor bez importů. Adresa workeru jde přes politiku Trusted Types a CSP dostane i `trusted-types svelte-trusted-html nylzo-kdf`, aby stránka nemohla vytvořit jinou politiku. Stojí to zhruba 125 ms na odvození na M4 Pro a druhý JS soubor v sestavení.
- B. Hlavní vlákno jako dnes v `@nylzo/core`. CSP se nemění. Paměť po několika pokusech závisí na úklidu JavaScriptCore a na iPhonu může stránku ukončit.

Doporučení: A. Paměť pak nezávisí na úklidu paměti a na počtu pokusů, což chrání i slabší iPhony, které neotestujeme. Pokud iPhone v zkoušce OD6 přežije 10 odvození na hlavním vlákně, zůstává B jako možnost s menší změnou. Rozhodnuto: A.

### D29: rozsah PoC

Doporučení: rozsah podle sekce Rozsah. Kostru obrazovek píšu já, aby e2e testy měly co ověřovat; vlastník ji pak přestyluje. Rozhodnuto podle doporučení.

### OD6: brána před psaním UI

Výsledek z iPhonu čeká. Navržené kritérium úspěchu: medián odvození profilu 1 do 3 s na vlastníkově iPhonu a žádné přerušení stránky během 10 odvození za sebou v režimu, který vybere D28. Pokud iPhone neprojde ani s workerem, rozhodne vlastník o výchozím profilu veřejné instance (profil 2, PBKDF2) dřív, než začne krok 6.

## Rozložení repozitáře (změny)

```
web/pnpm-workspace.yaml         přidá app a e2e
web/core/src/pow.ts             PoW digest, počet nulových bitů, formát tokenu (vektory)
web/app/                        @nylzo/web: Svelte 5, Vite 8, Tailwind 4 (D12, D15)
  index.html                    šablona se zástupným nonce (html.cspNonce)
  vite.config.ts                jeden JS a jeden CSS (plus worker při D28 A), assetsInlineLimit 0
  src/main.ts                   výběr obrazovky podle location.pathname, bez routeru
  src/lib/api.ts                klient /api/v1: výzvy, podpisy, chyby podle §6.1, jedno opakování po 404 (§5.5)
  src/lib/create.ts             stavový automat vytvoření
  src/lib/read.ts               stavový automat čtení
  src/lib/pow.ts                řešič PoW
  src/lib/kdf-worker.ts         scrypt a PoW ve workeru (jen D28 A)
  src/lib/download.ts           Blob jako application/octet-stream, očištěný název souboru
  src/lib/i18n.ts               typovaný slovník cs a en
  src/screens/*.svelte          Create, Created, Read: kostra bez vzhledu
  test/                         Vitest
web/e2e/                        Playwright proti skutečnému nylzod
internal/core/pow.go            PoW digest a nulové bity, sdílí server a později CLI
internal/server/static.go       servírování vložených souborů klienta
internal/server/web/            zástupná stránka v gitu, dist/ z Vite mimo git
```

Go vkládá adresář `internal/server/web`. Když v něm je `dist/index.html`, server servíruje sestaveného klienta, jinak zástupnou stránku jako dnes. `go build` a `go test` tak fungují i bez sestavení webu a CI sestaví web před e2e testy.

## Kroky

1. Ověření sestavení: nejmenší aplikace Svelte 5 a Vite 8 sestavená do `nylzod` a načtená s produkční CSP v Chromiu, Firefoxu a WebKitu přes Playwright. Ověří, že nevzniká žádné porušení CSP, že Svelte projde s Trusted Types, že server nahradí zástupný nonce a že bundle neobsahuje `svelte/compiler`, `eval` ani `new Function`. Kdyby cokoli vyžadovalo uvolnit CSP, zastavím se a zeptám se vlastníka.
2. Server: servírování `index.html` s nonce na `/` a `/m/{id}`, JS a CSS se správným `Content-Type` a ostatní cesty 404; odpovědi mimo HTML dál nesou `default-src 'none'`. Při D28 A nejdřív změna spec §9 a návrhu, pak serveru. Testy v Go.
3. PoW v jádrech: `internal/core` dostane výpočet digestu a nulových bitů, server ho použije, generátor přidá vektory `pow` do `spec/vectors.json` a `web/core` je projde (AK9 hlídá obě strany).
4. `api.ts` s testy nad podvrženým `fetch`.
5. Stavové automaty vytvoření a čtení s testy: opakování po 403 `id_token` a 409 s novým id; claim až po kliku a jen při viditelné stránce mimo prerender (AK16); `kdf_id` z fragmentu a zastavení bez podpisu ověřovače, když server hlásí jiný profil (AK24); značka v sessionStorage během KDF, aby reload po ukončení stránky systémem ukázal hlášku „toto zařízení nezvládne odvození klíče“ místo nového pokusu.
6. Kostra obrazovek a slovník (po bráně OD6).
7. Playwright e2e proti `nylzod` se vloženým klientem, podle tabulky níže, včetně toku s PoW (`NYLZO_GLOBAL_DAILY_CREATES` nastavený tak, aby PoW byl povinný).
8. CI: job pro web s `svelte-check`, Vitest, sestavením, kontrolou bundle a rozpočtu velikosti, sestavením `nylzod` s klientem a Playwrightem ve třech enginech.
9. README (sestavení a spuštění klienta), návrh (výsledky D19, D28, D29, Next Steps 5), PR.

Kroky 1 až 5 nezávisí na OD6 a mohou začít hned po schválení. Krok 6 čeká na bránu OD6.

## Akceptační kritéria

| AK | Jak to PoC ověří |
|---|---|
| AK2 | Playwright zaznamená každý požadavek při vytvoření i čtení (URL, hlavičky, tělo); žádný neobsahuje S, fragment ani passphrase a žádný nenese Referer |
| AK3 | Test vytvoří zprávu přes `@nylzo/core` a M si nechá; prohlížeč načte `/m/{id}` a stáhne metadata; stav přes M pak ukáže `opens_used` 0 |
| AK10 | Hlavičky na odpovědích HTML, JS, CSS a API; CSP bez `unsafe-eval` a `data:`; dvě načtení HTML mají jiný nonce; celý tok vytvoření a čtení v Chromiu, Firefoxu a WebKitu s posluchačem `securitypolicyviolation`, který test shodí. Ruční ověření v Safari na macOS a na iPhonu spolu s OD6 |
| AK13 | Přílohy `.svg`, `.html` a `.pdf` skončí událostí stažení s očištěným názvem a typem `application/octet-stream`, stránka nikam nenaviguje |
| AK16 | `Sec-Purpose` na `/m/*` a `/api/*` vrací 503; jednotkový test brány claimu (`document.prerendering`, `visibilityState`); prerender přes Speculation Rules v Chromiu claim nevyvolá. Kdyby Playwright prerender spustit neuměl, zůstane tahle část ruční a PR to uvede |
| AK18 | CLS 0 při vytvoření i čtení na 430 × 932 a 1440 × 900 (PerformanceObserver `layout-shift`) |
| AK19 | Skript nad sestaveným CSS: `@keyframes` a `transition` jen s `transform` a `opacity`, ke každé animaci pravidlo pro `prefers-reduced-motion`; inline styly a vložené `<style>` zachytí CSP v režimu enforce. Použitelnost bez `backdrop-filter` ověří vlastník, až bude sklo v návrhu |
| AK20 | axe v Playwrightu (kontrast 4,5:1), interaktivní prvky nejméně 44 × 44 px na 430 × 932, JS do 150 kB gzip, CSS do 40 kB gzip |
| AK24 (e2e část) | Playwright podvrhne v metadatech jiný `kdf_id`; klient se zastaví a hlavičku `X-Nylzo-Verifier` nepošle |

## Závislosti

Runtime: `svelte` a `@noble/hashes` (D12). Vývojové: `vite`, `@sveltejs/vite-plugin-svelte`, `tailwindcss`, `@tailwindcss/vite`, `svelte-check`, `@playwright/test`, `@axe-core/playwright`. Verze z návrhu (B5) před přidáním ověřím v npm registru; pnpm drží `minimumReleaseAge` 1440 minut a prázdné `allowBuilds`. Playwright v CI stahuje své prohlížeče, verze je připnutá.

## Rizika

- Paměť scryptu na iPhonu: řeší D28 a brána OD6.
- Politika `svelte-trusted-html` pouští šablony do `innerHTML` beze změny. Bezpečné je to jen bez `{@html}`, proto zákaz `{@html}` hlídá kontrola v CI.
- Worker s CSP `default-src 'none'` nesmí nic importovat; krok 1 to ověří na sestavení.
- WebKit v Playwrightu není Safari a neběží na iOS, proto ruční ověření v AK10.
- `fetch` neukazuje průběh nahrávání; PoC ukáže nahrávání 5 MB bez procent, průběh stahování ze streamu ano. XMLHttpRequest s průběhem nahrávání je možné doplnit, pokud ho vlastník v návrhu chce.
