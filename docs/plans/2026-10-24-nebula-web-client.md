# Plán: webový klient ve vzhledu Nebula

Datum: 24. 10. 2026. Větev: `feat/nebula-web-client` z `main`. Stav: implementace návrhu, který vlastník schválil 24. 10. 2026 (artefakt „Nylzo nebula“, verze v54: `Main`, `Phone`, `Open`, `OpenPhone`, `OpenStates`).

## Rozsah

Úvodní stránka (editor zprávy a všechny sekce pod ním) a stránka pro otevření zprávy dostávají vzhled ze schváleného návrhu. Obrazovky `/sent`, `/report` a `/transparency` fungují stejně jako dosud, jen dostanou nový rám (hlavička, patička, sklo) a styl z vrstvy `.legacy-screen`. Protokol, server ani CLI se nemění.

## Rozhodnutí

D73: návrh v kódu. Svelte 5 v režimu runes a Tailwind CSS 4.3.3, žádná knihovna komponent. Písma Geist, Geist Mono a Sora jsou self-hosted balíčky `@fontsource-variable` 5.3.0 (OFL) jen se subsety latin a latin-ext, kvůli CSP a soukromí. Build přidá preload pro Geist a Soru a klient před prvním vykreslením počká na písma nejvýš 1,5 s, aby výměna písma neposunula rozvržení (AK18).

D74: pohyb jen přes opacitu a transformace (AK19). Barva se nemění přechodem, ale prolnutím dvou vrstev (přepínač, kroky průběhu, tlačítko kopírování). Průběh šifrování a otevírání kreslí element `<progress>` stylovaný přes pseudo-elementy, protože CSP nedovolí atribut `style`. Skryté panely a ovládací prvky zůstávají v DOM s `inert` a nulovou opacitou, takže se při změně stavu rozvržení nehne. Playwright `inert` za skryté nepovažuje, testy proto hledají jen prvky mimo `inert` (`live()` v `web/e2e/tests/helpers.ts`).

D75: poctivé tempo animací. Šifrování trvá na obrazovce nejméně 5 s, otevření nejméně 3,8 s s passphrase a 2,6 s bez ní (při `prefers-reduced-motion` 1,2 s a 1 s). Krok ani průběh nikdy nepředběhne skutečnou práci (`web/app/src/lib/pacing.ts`) a chyba se ukáže hned, bez čekání na konec animace.

D76: formulář nabízí jen scrypt. Volba PBKDF2 zmizela ve shodě s D71 (PR 26). Odkazy s PBKDF2 klient na této větvi dál otevře, dokud D71 nepřijde do `main`.

D77: klient zůstává jeden JS balík. Rozdělení podle stránek by na mobilu ušetřilo zhruba 40 KiB nepoužitého JS, ale změnilo by zavádění modulů pod CSP jen s nonce a přidalo další soubory, které hlídá `verify-client`. Za dva až tři body v Lighthouse to nestojí.

## Odchylky od návrhu

- Ovládací prvky mají na mobilu 44 px místo 40 px (AK20): pole passphrase, akce pod kartou, záložky v sekcích Bezpečnost a Vlastní server.
- Sklo je na víc plochách, než doporučuje sekce „Vizuální design a interakce“ (dvě až tři na obrazovce). Takový návrh vlastník schválil a měření níže problém s výkonem nenašlo.
- Nahlášení zneužití v sekci Hranice je samostatné tlačítko místo odkazu ve větě, aby mělo na mobilu 44 px.
- Procenta v tlačítku průběhu mají pevnou šířku čtyř znaků a skryté akce pod kartou si drží místo. V návrhu se šířka měnila s počtem číslic a akce na mobilu mizely z řádku, což by porušilo AK18.

## Měření 24. 10. 2026

Lighthouse 13.5.0 v Chromu proti `nylzo serve` spuštěnému lokálně přes HTTP:

| Stránka | Výkon | Přístupnost | Doporučené postupy | SEO |
| --- | --- | --- | --- | --- |
| `/`, mobil | 94 | 100 | 96 | 60 |
| `/`, desktop | 100 | 100 | 96 | 60 |
| `/m/…`, mobil | 95 | 100 | 96 | 60 |
| `/m/…`, desktop | 100 | 100 | 96 | 60 |

CLS je všude 0. TBT na mobilu je 110 ms na úvodní stránce a 10 ms při otevření zprávy. SEO snižuje `noindex`, který server posílá záměrně. Doporučené postupy strhávají body za 404 na `/favicon.ico` (řeší PR 27) a za vypnutou back/forward cache kvůli `Cache-Control: no-store`, což je také záměr.

Balík: JS 112 kB gzip (rozpočet 150 kB), CSS 16 kB gzip (rozpočet 40 kB).

Hlavičky na HTML, API i assetech: CSP s nonce, `default-src 'none'` a Trusted Types, HSTS, COOP a CORP `same-origin`, `Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`, `Permissions-Policy` a `X-Robots-Tag`.

## Otevřené pro vlastníka

- Assety s hashem v názvu chodí s `Cache-Control: no-store` (D36), takže každá návštěva stahuje zhruba 270 kB znovu. Cache s `immutable` by opakované návštěvy zrychlila; rozhodnutí patří vlastníkovi.
