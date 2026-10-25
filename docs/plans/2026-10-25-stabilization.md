# Plán: stabilizace kódu

Datum: 25. 10. 2026. Větev: `refactor/stabilize` z `main`. Stav: vlastník 25. 10. 2026 požádal o zjednodušení celého repozitáře, přísné lintery, přesné pojmenování bez zkratek, moderní syntaxi, přehlednou adresářovou strukturu a jen kritické testy. Chování, protokol a bezpečnostní vlastnosti se nemění.

## Výchozí stav

Go má asi 12 tisíc řádků kódu a 13 tisíc řádků testů, TypeScript a Svelte 10 tisíc a 7 tisíc. Balíček `internal/server` má 21 zdrojových a 31 testových souborů v jedné složce a míchá HTTP handlery, rate limiting, proof of work, podpisové výzvy, konfiguraci, logování a vestavěného webového klienta; `api.go` má 966 řádků. Webová aplikace má 27 modulů v `src/lib` a 20 testů v `test/` naplocho. Web neměl ESLint ani formátovač, kontroloval jen `tsc`, `svelte-check` a vlastní skript se zakázanými vzory. V kódu je hodně zkratek (`rec`, `m`, `a`, `la`, `cfg`, `deps`).

## Rozhodnutí

D87: lintery a formátovače jsou přísné a jejich verze jsou připnuté.
- Go: golangci-lint 2.15.0 se zapnutými lintery na chyby a bezpečnost (gosec, bodyclose, noctx, sqlclosecheck, rowserrcheck, nilerr, errorlint, musttag a další), moderní idiomy (modernize, usestdlibvars, perfsprint), složitost (cyclop 15, gocognit 15, funlen 80/50, nestif 4, dupl 100), pojmenování (revive, varnamelen, staticcheck ST1003) a testy (thelper, tparallel). Formátuje gofumpt, goimports a golines na 120 znaků. Exkluze se nesmí hromadit: `warn-unused`, `nolintlint` vyžaduje název linteru i důvod.
- Web: ESLint 10 s typescript-eslint `strictTypeChecked` a `stylisticTypeChecked`, eslint-plugin-svelte, unicorn (včetně `name-replacements` proti zkratkám), regexp, eslint-comments, perfectionist jen pro řazení importů, pravidla pro Vitest a Playwright. Formátuje Prettier s pluginy pro Svelte a Tailwind; knip hledá nepoužité soubory, exporty a závislosti.
- Repozitář: `.editorconfig`, actionlint, zizmor v pedantickém režimu a shellcheck. Dlouhé úlohy mise jsou shellové soubory v `mise-tasks/`, aby je shellcheck viděl.
- Nástroje instaluje mise z release assetů s kontrolními součty v `mise.lock`, takže CI i lokální běh používají stejné binárky.
- typescript-eslint zatím nepodporuje TypeScript 7 (chybí API), proto kořen `web/` drží TypeScript 6.0.3 pro lintování a svelte-check, zatímco jádro a e2e kontroluje `tsc` 7.
- Zamítnuto: wsl a nlreturn (podle maratoriho konfigurace kód nečtou lépe), exhaustruct (bojuje s nulovými hodnotami Go), err113, wrapcheck a paralleltest (hodně falešných nálezů), unicorn `consistent-boolean-name` (Go ani Svelte předpony `is` a `has` nepoužívají a repozitář má zůstat jednotný). Oxlint a Biome zatím neumí šablony Svelte a oxfmt je před verzí 1.0; podíváme se na ně po vydání TypeScriptu 7.1.

D88: názvy se vypisují celé. Výjimkou jsou jen idiomy jazyka v malém rozsahu, vyjmenované v `CLAUDE.md` (v Go například `ctx`, `err`, `w` a `r` v handlerech, krátké receivery). Hlídá to varnamelen, revive a ST1003 v Go a unicorn `name-replacements` na webu.

D89: kód je rozdělený podle odpovědnosti, ne podle typu souboru, a žádná složka nemá víc než zhruba 15 souborů. Testy leží vedle kódu, který testují.
- `internal/server` se dělí na podbalíčky (vestavěný webový klient, rate limiting, proof of work a výzvy, hlášení zneužití, konfigurace a další), HTTP vrstva zůstává v `server` a `api.go` se dělí podle skupin endpointů.
- Webová aplikace má `src/app/`, `src/features/` (create, read, sent, report, transparency, home) a `src/shared/`. Závislosti vedou shared → features → app.

D90: testujeme jen kritické věci: testovací vektory protokolu (AK9), bezpečnostní záruky a akceptační kritéria podle AK, pravidla specifikace a regrese, které se už jednou rozbily. Duplicitní scénáře, testy triviálních pomocných funkcí, textů a toho, co dokáže typový systém, mizí. E2E testy běží celé jen v Chromiu; Firefox a WebKit spouští jen testy, jejichž chování závisí na prohlížeči (Web Crypto, IndexedDB, CSP a Trusted Types, workery, stahování), označené tagem `@cross-browser`. Z 99 testů ve třech prohlížečích (297 běhů) zbylo 52 v Chromiu a 10 ve Firefoxu a WebKitu (72 běhů); Chromium a WebKit lokálně doběhnou za 36 až 40 s místo 92 s. CI má místo sedmi shardovaných jobů z D81 tři, jeden na prohlížeč, a WebKit dál běží na macOS. Názvy jobů ztratily číslo shardu, takže povinné kontroly v ochraně větve `main` je potřeba přepsat.

D91: vlastní skript se zakázanými vzory (`forbidden.ts`) nahrazují pravidla ESLintu (`no-restricted-properties`, `no-eval`, `no-new-func`, `no-implied-eval`, `svelte/no-at-html-tags`, `svelte/no-inline-styles`). `check-sources.ts` zůstává jen pro svelte-autofixer, který vyžaduje `CLAUDE.md`.

## Postup

1. Nástroje a konfigurace, mechanické formátování (Go: gofumpt, goimports, golines; web: Prettier) v samostatných commitech.
2. Refaktoring po oblastech: Go server a CLI; Go jádro, úložiště, pepper a klient; TypeScriptové jádro; webová aplikace; e2e. Exportované API cizí oblasti se během toho nemění, přejmenování napříč oblastmi přijdou na konec najednou.
3. Zkrácení testů až po refaktoringu, aby celá dosavadní sada hlídala, že se chování nezměnilo.
4. CI podle nových nástrojů a ověření celé sady, Lighthouse a e2e.

## Stav a předání (25. 10. 2026 dopoledne)

Sloučeno na `refactor/stabilize` a ověřené: nástroje a konfigurace (D87), TypeScriptové jádro, e2e (D90), Go jádro, pepper, úložiště a klient. Všechny testy v Go s race detektorem prochází, `spec/vectors.json` se generuje beze změny.

Cestou opravené chyby (každá ve vlastním commitu):
- SQLite úložiště tiše ořezávalo sloupec, který se nevešel do typu v Go, takže `kdf_id` 257 četlo jako profil 1. Takový řádek teď hlásí jako poškozený.
- Klient přijímal v `X-Nylzo-Policy` `kdf_id` mimo rozsah bajtu jako profil 1. Teď ho odmítne. Odvození klíče se řídí profilem z fragmentu, takže nešlo o downgrade.

Rozpracované větve (zálohy na GitHubu):
- `wip/stabilize-go-server`: rozdělení `internal/server` na podbalíčky `webclient`, `ratelimit`, `proof`, `config`, `abuse`, `sweep`, `logging`, `diskspace`, handlery podle skupin endpointů, `internal/cli` s balíčkem `console`. Vychází z `1f870a6`. Zbývají jména, nulový lint v oblasti a zkrácení testů. Při zkracování musí zůstat `TestSecurityHeadersAndNonce`, `TestBuiltClientIsServed`, `TestRobotsAndSitemap`, `TestHomePagesHaveTheirOwnAddresses` a testy hlaviček transparentnosti, protože e2e se na ně spoléhá.
- `wip/stabilize-web-app`: struktura `src/app`, `src/features`, `src/shared`, `forbidden.ts` nahrazený ESLintem (D91), rozdělené `Read.svelte` a `Composer.svelte`. Vychází z `da0e3a3`. Zbývají jména, nulový ESLint, knip a zkrácení testů. Platí pravidla pro Svelte z `CLAUDE.md`.

Jak navázat:
1. Dokončit obě větve, nebo převzít jejich stav. Commity přenést na `refactor/stabilize` přes `git cherry-pick <základ>..<větev>`.
2. Očekávané konflikty: cesta k `app/src/lib/api.ts` v `web/e2e/tests/support/messages.ts` a `web/e2e/tests/create.spec.ts`, pokud ji aplikace přesune. Commit „resolve app modules like the bundler“ je na obou větvích stejný.
3. Přejmenování napříč oblastmi najednou: v Go přes `gopls rename` (v0.24.0), v TypeScriptu ručně s kontrolou typů. Seznam je níže. S ním zmizí 12 dočasných `//nolint:gocritic`.
4. Ověřit: `mise run lint` s nulovými nálezy, `mise run test` (včetně AK21 a AK22 zvlášť), build klienta, e2e v Chromiu a WebKitu, Lighthouse 100 na `/` a `/cs/` na mobilu.
5. Aktualizovat popis PR a v ochraně větve `main` přepsat povinné kontroly na nové názvy e2e jobů.

Přejmenování napříč oblastmi:
- `@nylzo/core`: `ID_LEN`, `HEADER_LEN`, `CHALLENGE_LEN`, `PUBLIC_KEY_LEN` a vnitřní `*_LEN` na `*_LENGTH`; `MIN_PASSPHRASE_CHARS` na `MIN_PASSPHRASE_CHARACTERS`; `MAX_MIME` na `MAX_MIME_BYTES`; `LinkKeys.r` a `.saltPw` na `readSeed` a `passphraseSalt`; `ContentKeys.k` a `.v` na `contentKey` a `verifierSeed`; `Fragment.s` a `Link.s` na `secret`; `anyPassphraseKey` (vrací Promise) na synchronní `passphraseKey`.
- `core`: `SolvePoW` vrací `(string, error)` a generátor si nechá vnitřní variantu s čítačem; metody `Signature` `Verify`, `Authorization` a `VerifierHeader` s ukazatelovým receiverem; `Signature.Sig` na `Value`; `LinkKeys.SaltPW` na `PassphraseSalt`; `TokenLen` na `SeedLen`; `MinPassphraseChars` na `MinPassphraseCharacters`.
- `pepper`: `Ring.MAC` vrací strukturu `{MAC, PepperID}`.
- `store`: `Messages.MarkEnding` vrací `store.Ending{State, Ref, Recorded}`; `Messages.Ack`, `RecordDownload` a `AckLease` berou `*store.Lease`; `Counters.RateReserve` vrací `(int64, error)` s nulou pro odmítnutí; `Reconcile` vrací `store.ReconcileReport{Orphans, Listed}`; `ClaimParams` na `ClaimParameters`.
- `client`: `Create` bere `*CreateRequest`. `disk`: `Dir` na `Directory`.

## Zdroje

- golangci-lint 2.15.0 a jeho lintery a formátovače: https://github.com/golangci/golangci-lint/releases , https://golangci-lint.run/docs/linters/ , https://golangci-lint.run/docs/formatters/
- Instalace binárky místo `go tool`: https://golangci-lint.run/docs/welcome/install/local/
- Vzorová přísná konfigurace: https://github.com/maratori/golangci-lint-config
- Novinky Go 1.25 až 1.27: https://go.dev/doc/go1.25 , https://go.dev/doc/go1.26 , https://go.dev/doc/go1.27
- Rozvržení modulu a názvy balíčků: https://go.dev/doc/modules/layout , https://go.dev/blog/package-names , https://google.github.io/styleguide/go/best-practices#package-size
- actionlint, zizmor, shellcheck: https://github.com/rhysd/actionlint , https://docs.zizmor.sh/usage/ , https://github.com/koalaman/shellcheck
- ESLint 10 a typescript-eslint: https://eslint.org/blog/2026/02/eslint-v10.0.0-released/ , https://typescript-eslint.io/users/configs , https://typescript-eslint.io/users/dependency-versions
- TypeScript 7 bez API pro nástroje: https://github.com/typescript-eslint/typescript-eslint/issues/12518 , https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/
- eslint-plugin-svelte: https://sveltejs.github.io/eslint-plugin-svelte/user-guide/ , unicorn `name-replacements`: https://github.com/sindresorhus/eslint-plugin-unicorn
- Podpora Svelte v Oxlintu a Biome: https://oxc.rs/docs/guide/usage/linter/js-plugins.html , https://biomejs.dev/internals/language-support/
- Prettier a pluginy: https://prettier.io/docs/configuration , https://github.com/tailwindlabs/prettier-plugin-tailwindcss
- Novinky Svelte 5: https://svelte.dev/docs/svelte/declaration-tags , https://svelte.dev/docs/svelte/context
- Testy ve Svelte a Playwright: https://svelte.dev/docs/svelte/testing , https://playwright.dev/docs/best-practices , https://playwright.dev/docs/test-sharding
