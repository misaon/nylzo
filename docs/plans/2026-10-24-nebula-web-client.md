# Plán: webový klient ve vzhledu Nebula

Datum: 24. 10. 2026. Větev: `feat/nebula-web-client` z `main`. Stav: implementace návrhu, který vlastník schválil 24. 10. 2026 (artefakt „Nylzo nebula“, verze v54: `Main`, `Phone`, `Open`, `OpenPhone`, `OpenStates`).

## Rozsah

Úvodní stránka (editor zprávy a všechny sekce pod ním) a stránka pro otevření zprávy dostávají vzhled ze schváleného návrhu. Obrazovky `/sent`, `/report` a `/transparency` fungují stejně jako dosud, jen dostanou nový rám (hlavička, patička, sklo) a styl z vrstvy `.legacy-screen`. Protokol, server ani CLI se nemění.

## Rozhodnutí

D73: návrh v kódu. Svelte 5 v režimu runes a Tailwind CSS 4.3.3, žádná knihovna komponent. Písma Geist, Geist Mono a Sora jsou self-hosted balíčky `@fontsource-variable` 5.3.0 (OFL) jen se subsety latin a latin-ext, kvůli CSP a soukromí. Build přidá preload pro Geist, Geist Mono a Soru a klient před prvním vykreslením počká na písma nejvýš 1,5 s, aby výměna písma neposunula rozvržení ani nezměnila rozměr tlačítek (AK18).

D74: pohyb jen přes opacitu a transformace (AK19). Barva se nemění přechodem, ale prolnutím dvou vrstev (přepínač, kroky průběhu, tlačítko kopírování). Průběh šifrování a otevírání kreslí element `<progress>` stylovaný přes pseudo-elementy, protože CSP nedovolí atribut `style`. Skryté panely a ovládací prvky zůstávají v DOM s `inert` a nulovou opacitou, takže se při změně stavu rozvržení nehne. Playwright `inert` za skryté nepovažuje, testy proto hledají jen prvky mimo `inert` (`live()` v `web/e2e/tests/helpers.ts`).

D75: poctivé tempo animací. Šifrování trvá na obrazovce nejméně 5 s, otevření nejméně 3,8 s s passphrase a 2,6 s bez ní (při `prefers-reduced-motion` 1,2 s a 1 s). Krok ani průběh nikdy nepředběhne skutečnou práci (`web/app/src/lib/pacing.ts`) a chyba se ukáže hned, bez čekání na konec animace.

D76: formulář nabízí jen scrypt. Volba PBKDF2 zmizela ve shodě s D71 (PR 26). Odkazy s PBKDF2 klient na této větvi dál otevře, dokud D71 nepřijde do `main`.

D77: klient zůstává jeden JS balík. Rozdělení podle stránek by na mobilu ušetřilo zhruba 40 KiB nepoužitého JS, ale změnilo by zavádění modulů pod CSP jen s nonce a přidalo další soubory, které hlídá `verify-client`. Za dva až tři body v Lighthouse to nestojí.

D78: příjemce může text zprávy zkopírovat jedním tlačítkem. Schránka ale nepatří stránce: text v ní zůstane i po zavření karty a správce schránky nebo synchronizace mezi zařízeními si ho může uložit. Po zkopírování proto stránka oznámí „Text je ve schránce. Zůstane tam i po zavření stránky.“, aby příjemce věděl, že ho má ze schránky sám smazat.

D79: čtečka potvrdí otevření (`ack`) a smaže fragment z adresy až ve chvíli, kdy stránka dešifrovanou zprávu opravdu zobrazí. Dřív to udělala hned po dešifrování, ještě během animace otevírání, takže kdo stránku v tu chvíli opustil, spotřeboval otevření a zprávu nikdy neviděl. Teď si čtečka při odchodu před zobrazením nechá lease i fragment a po návratu na stejnou kartu může zprávu načíst znovu, dokud lease běží (`shown` v `web/app/src/lib/read.ts`).

## Odchylky od návrhu

- Ovládací prvky mají na mobilu 44 px místo 40 px (AK20): pole passphrase, akce pod kartou, záložky v sekcích Bezpečnost a Vlastní server.
- Sklo je na víc plochách, než doporučuje sekce „Vizuální design a interakce“ (dvě až tři na obrazovce). Takový návrh vlastník schválil a měření níže problém s výkonem nenašlo.
- Nahlášení zneužití v sekci Hranice je samostatné tlačítko místo odkazu ve větě, aby mělo na mobilu 44 px.
- Ruční výběr v sekcích Jak to funguje a Kdo co vidí zastaví automatické přepínání natrvalo, jak slibuje text „Klepnutím na kteroukoli je zastavíte.“. Dřívější obnovení po 15 s nedávalo uživateli trvalé zastavení, které WCAG 2.2.2 požaduje.
- Procenta v tlačítku průběhu mají pevnou šířku čtyř znaků a skryté akce pod kartou si drží místo. V návrhu se šířka měnila s počtem číslic a akce na mobilu mizely z řádku, což by porušilo AK18.

## Opravy z review

Review podle gstack (specialisté, adversariální průchod a druhé nezávislé kolo nad opravami) našlo chyby, které jsou opravené v této větvi a pokryté testy:

- Editor po vytvoření odkazu smaže text, passphrase i přílohu a zprávu si zařízení zapamatuje hned po vytvoření, ne až po animaci.
- Zapnutá ochrana s prázdnou passphrase je chyba formuláře, ne zpráva bez passphrase.
- Přetažený text už neodstraní vybranou přílohu. Po selhání vytvoření se fokus vrátí na tlačítko Vytvořit odkaz.
- Stránka pro otevření nepřijme druhé Enter během otevírání, po odchodu zahodí čekající dešifrovaný pohled a selhané smazání ukáže viditelně.
- Dvojklik na Stáhnout nebo Přesto stáhnout uloží soubor jen jednou, dvojklik u rizikového souboru nechá upozornění otevřené a Nestahovat vrátí fokus na tlačítko Stáhnout. Dlouhá „přípona“ (víc než 16 znaků) se zkracuje se jménem souboru a nevyteče z karty.
- Po odeslaném nahlášení zůstane popover ve stavu „odesláno“, dokud příjemce zprávu nezobrazí. Po zobrazení může nahlásit znovu (D41). Zavřený a znovu otevřený popover si nechá rozepsaný důvod i výsledek odesílání.
- Části odkazu v sekci Jak to funguje mají přístupný název, který obsahuje viditelný text (WCAG 2.5.3).
- Automatické přepínání sekcí se pozastaví při najetí myší a při fokusu z klávesnice uvnitř, při `prefers-reduced-motion` neběží a ruční výběr ho zastaví natrvalo (WCAG 2.2.2).
- Úvodní stránka při `pagehide` vymaže rozepsanou zprávu, passphrase, soubor i hotový odkaz, odpověď na vytvoření po odchodu už nic nezobrazí ani nezapamatuje a po návratu přes mezipaměť zpět/vpřed se stránka načte znovu, stejně jako stránka pro otevření.
- Výpočet doby útoku hrubou silou používá pro Groverův algoritmus (π/4)·2¹²⁸ iterací a slova pro miliony a miliardy let přes slovník, ne napevno v kódu.

QA podle gstack (`/qa`, headless Chromium, desktop a 430 × 932) našlo jednu další chybu: při šifrování s passphrase krok Odvození na chvíli ukázal popis pro zprávu bez passphrase, protože editor už passphrase smazal. Popis teď vychází z toho, s čím šifrování začalo, a hlídá ho e2e test. Dvě drobnosti zůstávají na vlastníkovi: titulek „Máte šifrovanou zprávu“ nad všemi oznámeními a dva formáty data platnosti (panel po vytvoření a stránka pro otevření).

E2E testy běží ve výchozím stavu s `prefers-reduced-motion: reduce`. Nekonečné dekorativní animace (mlhovina, hvězdy, rotující okraj) se v CI bez GPU skládají softwarově a zpomalily celou sadu z 3,5 na 21 minut, takže jednotlivé testy narážely na limit 60 s. Testy, které na pohybu závisí (AK18, odkrývání textu, odchod během otevírání, popis kroků při šifrování, automatické přepínání), si plný pohyb zapínají samy.

Testy runes s efekty běží ve Vitest v projektu `client` s vlastním prostředím (`web/app/test/client-environment.ts`), ostatní unit testy dál v Node.

## Měření 24. 10. 2026

Lighthouse 13.5.0 v Chromu proti `nylzo serve` spuštěnému lokálně přes HTTP:

| Stránka | Výkon | Přístupnost | Doporučené postupy | SEO |
| --- | --- | --- | --- | --- |
| `/`, mobil | 95 | 100 | 96 | 60 |
| `/`, desktop | 100 | 100 | 96 | 60 |
| `/m/…`, mobil | 96 | 100 | 96 | 60 |
| `/m/…`, desktop | 100 | 100 | 96 | 60 |

CLS je všude 0. Na mobilu je LCP 2,9 s na úvodní stránce a 2,7 s při otevření zprávy, TBT 100 ms a 0 ms. Měřeno po opravách z review, včetně preloadu Geist Mono. SEO snižuje `noindex`, který server posílá záměrně. Doporučené postupy strhávají body za 404 na `/favicon.ico` (jediná chyba v konzoli, řeší PR 27) a za vypnutou back/forward cache kvůli `Cache-Control: no-store`, což je také záměr.

Balík: JS 112 kB gzip (rozpočet 150 kB), CSS 16 kB gzip (rozpočet 40 kB).

Hlavičky na HTML, API i assetech: CSP s nonce, `default-src 'none'` a Trusted Types, HSTS, COOP a CORP `same-origin`, `Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`, `Permissions-Policy` a `X-Robots-Tag`.

## Otevřené pro vlastníka

- Assety s hashem v názvu chodí s `Cache-Control: no-store` (D36), takže každá návštěva stahuje zhruba 270 kB znovu. Cache s `immutable` by opakované návštěvy zrychlila; rozhodnutí patří vlastníkovi.
- Stránka pro otevření má nad každým oznámením titulek „Máte šifrovanou zprávu“, i když zpráva neexistuje nebo už byla otevřená. Návrh to tak má; jiný titulek pro oznámení je volba vlastníka.
- Platnost se ukazuje ve dvou formátech: dlouhý na panelu po vytvoření, krátký na stránce pro otevření.
