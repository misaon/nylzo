# Plán: milník 1, protokol v1, jádra Go a TS, server nylzod (sqlite+disk)

Datum: 22. 10. 2026. Větev: `claude/gracious-chebyshev-79747d`. Zdroj pravdy: `docs/designs/nylzo-architecture-research.md` (APPROVED 21. 10. 2026), přílohy A, E, H a tři `review-*.md`. Otevřená rozhodnutí OD1 až OD24 platí v navrženém výchozím stavu.

Rozhodnutí vlastníka (22. 10. 2026): Go modul `github.com/misaon/nylzo`, licence MIT, monorepo.

## Cíl milníku

Protokol v1 zapsaný normativně, dvě jádra (Go, TypeScript) svázaná vektory, server `nylzod` s adaptérem sqlite+disk a testy podle akceptačních kritérií, které jdou ověřit bez webového klienta, bez CLI a bez nasazení. Hotovo znamená zelené testy lokálně i v CI a otevřený PR proti `main`.

Mimo rozsah: webový klient a UI (vlastník), CLI, MCP, adaptér postgres+s3 (čeká na R13), Dockerfile, image, podpisy, nasazení.

## Rozložení repozitáře

```
LICENSE                      MIT
README.md                    český návod: spuštění se SQLite, generování vektorů
go.mod                       module github.com/misaon/nylzo, go 1.27
spec/protocol.md             normativní protokol v1
spec/vectors.json            generuje `go run ./cmd/nylzo-vectors -out spec/vectors.json`
internal/core/               odvození (HKDF), KDF profily, obálka, URL, kanonický meta JSON
internal/pepper/             klíče podle účelu z pepperu, mac(), dummy mac, kyblík IP, rotace
internal/store/              rozhraní Store a Blobs, typy, chyby
internal/store/sqlite/       adaptér SQLite (modernc.org/sqlite) + disk bloby
internal/store/storetest/    sdílená testovací sada pro adaptéry (souběh, lease, pád)
internal/server/             net/http handlery, middleware, konfigurace, limity, PoW, logování
cmd/nylzod/                  main: `nylzod` (serve) a `nylzod sweep-once`
cmd/nylzo-vectors/           generátor spec/vectors.json
web/core/                    pnpm balíček @nylzo/core (TS 7, Web Crypto, @noble/hashes scrypt)
.github/workflows/ci.yml     Go, TS, křížová kontrola vektorů, govulncheck, pnpm audit
```

Tok dat a hranice:

```
  klient (web/core, později CLI)              nylzod (internal/server)            úložiště
  ─────────────────────────────              ────────────────────────            ────────
  id,S,M ← CSPRNG                              X-Nylzo-Create → parse
  R,salt_pw ← HKDF(hsalt,S)                    Content-Length 411/400/413
  P ← scrypt|PBKDF2(pass,salt_pw)              token bucket → RateHit (DB)
  K,V ← HKDF(hsalt,S||P)                       PoW (při 50 % stropu)
  objekt ← AES-GCM(K, aad=hdr||id||policy)     mac_read(h(R)), mac_verifier(h(V)),
  POST objekt + h(R),h(V),h(M) ───────────────▶ mac_manage(h(M)) ──────────────▶ messages + blob
  GET meta (Bearer R) ────────────────────────▶ mac porovnání, dummy při 404
  POST claim (R, V) ──────────────────────────▶ Claim(): tx BEGIN IMMEDIATE ───▶ opens−1, lease
  GET blob (R, L) ◀── objekt ─────────────────  CheckLease, ≤3 stažení
  POST ack ───────────────────────────────────▶ Ack → ReleaseIfDone → Blobs.Delete → ClearBlob
```

## Kroky

1. `spec/protocol.md`: přepis sekcí Protokol v1, Datový model, API, Logování, Ochrana proti zneužití do normativního textu (MUST/SHOULD). Jediné místo, kde dokument nechává detail otevřený a spec ho musí zafixovat: zakódování PoW tokenu (OD24 určuje algoritmus a vazbu, ne formát), kanonické kódování meta JSON (bajtová shoda obou jader) a formát hlavičky `X-Nylzo-Policy`. Tyto tři detaily plán označí v PR jako doplněné.
2. `internal/core`: `Derive` (hsalt, R, salt_pw, P, K, V), `Seal`/`Open` obálky s paddingem 4 KiB a 64 KiB, `ParseLink`/`FormatLink`, `EncodeMeta`/`DecodeMeta`. Testy: každé odvození proti vektorům, AK8 (1 bit, změna politiky, cizí id), fuzz `Open`, `DecodeMeta`, `ParseLink`, RFC 7914 vektory scrypt (čtvrtý s 1 GiB jen za `NYLZO_HEAVY_TESTS=1`).
3. `cmd/nylzo-vectors`: deterministický generátor (pevná semínka v kódu, ne CSPRNG), zapisuje `spec/vectors.json`; test v `internal/core`, že vygenerovaný obsah je shodný s commitnutým souborem (AK9, Go strana).
4. `internal/pepper`: `k_účel = HKDF(pepper, "nylzo/v1/mac/"+účel)` pro read, verifier, manage, lease, ip, pow; `Mac`, `Equal` (crypto/subtle), `Dummy` pro uniformní práci; `Bucket(now, addr)` = 24 bitů z HMAC(k_ip, datum UTC || prefix) pro /32, /24, /64, /48 po `netip` kanonizaci; `Ring` s aktuálním a předchozím pepperem a `pepper_id` (AK27).
5. `internal/store`: rozhraní `Store` (Create, Get, Claim, CheckLease, Ack, ReleaseIfDone, ClearBlob, Burn, Cancel, Status, Sweep, ReconcileBlobs, RateHit, PassphraseFail) a `Blobs` (Put, Get, Delete, List). Hodiny injektované (`Clock`), aby testy posouvaly čas bez čekání 120 s.
6. `internal/store/sqlite`: schéma messages, leases, passphrase_attempts, tombstones, rate_counters; WAL, `busy_timeout`, zápisy v `BEGIN IMMEDIATE`; `Claim` v jedné transakci (OD13 uvnitř); čítače `INSERT ... ON CONFLICT DO UPDATE ... RETURNING`; zánik řádku + náhrobek v jedné transakci; `Sweep` v dávkách po 1 000. Disk bloby: `data/blobs/{id}`, zápis přes dočasný soubor a rename, `Delete` bere ENOENT jako úspěch.
7. `internal/store/storetest`: sdílená sada (AK3, AK4 100×, AK5, AK6 včetně pádu mezi `ReleaseIfDone` a `Blobs.Delete` a dvojího Delete, AK7, AK27); sqlite ji spouští. Postgres ji později jen zapojí.
8. `internal/server`: konfigurace z proměnných `NYLZO_*`; `http.Server` s timeouty z dokumentu; routování Go 1.22 vzory; middleware: bezpečnostní hlavičky s CSP nonce per HTML odpověď, `Sec-Purpose` → 503, náhledové user agenty → HTML bez skriptu, token bucket 50/s burst 100 per instance, určení IP (`NYZLO_TRUSTED_PROXIES`, `NYLZO_PROXY_MODE=scaleway`); endpointy podle tabulky API; kontrola `Content-Length` před čtením těla; uniformní 404 s dummy HMAC; globální strop a PoW; časovač `ReleaseIfDone` na konci lease; vestavěný úklid (`NYLZO_SWEEP_INTERVAL`); `sweep-once`; logování přes `log/slog` bez tajemství a IP.
9. Testy serveru: AK1 (zachycený požadavek neobsahuje S, K, P, plaintext), AK3, AK4 přes HTTP, AK5, AK6 end-to-end, AK11 (sken logu), AK12 (429, 503 jen pro create, X-Forwarded-For za důvěryhodnou proxy), AK21 (bajtová shoda 404/409; prokládaných 1 000 vzorků × 3 třídy, medián a p95 v logu, práh 3× medián a 5× p95 podle D2), AK22 (200 pomalých spojení, lživý Content-Length, 411/400/413 před tělem), AK23 (kyblíky /64, /48, /24), fuzz hlavičky `X-Nylzo-Create`.
10. `web/core`: `@nylzo/core` bez frameworku: base64url, `derive` (HKDF přes Web Crypto, K jako neextrahovatelný `CryptoKey` přes `deriveKey`, R a V přes `deriveBits`), `kdf` (scrypt z `@noble/hashes/scrypt.js`, PBKDF2 z Web Crypto, NFC normalizace), `envelope` (AES-GCM z Web Crypto, padding, kanonický meta JSON), `link` (parsování fragmentu, kdf_id jen z fragmentu, `assertProfile` pro AK24). Vitest nad `spec/vectors.json` (shoda objektů bajt po bajtu díky pevnému IV ve vektoru, dešifrování, odmítnutí pozměněné obálky, RFC 7914).
11. CI: `.github/workflows/ci.yml` s akcemi připnutými na SHA; joby `go` (vet, test -race, govulncheck), `web` (pnpm install --frozen-lockfile, tsc, vitest, pnpm audit, kontrola `minimumReleaseAge` a zákazu build skriptů), `vectors` (regenerace a `git diff --exit-code spec/vectors.json`, pak TS testy nad výsledkem). Změna jednoho jádra bez druhého shodí CI (AK9).
12. README.md (česky), LICENSE, `.gitignore` pro `data/`; `/gstack-review`; PR s mapou AK.

## Pořadí a paralelizace

Kroky 1 až 4 jsou sériové (spec → jádro → vektory → pepper). Kroky 5 až 7 (store) a krok 10 (TS jádro) jsou nezávislé na sobě, závisí jen na vektorech. Krok 8 až 9 závisí na 4 až 7. Krok 11 až 12 nakonec.

## Rizika a jak je plán pokrývá

- Dvě jádra se rozejdou v detailu (KD15): vektory s diakritikou, kombinovanými znaky a NFC; kanonický meta JSON; CI křížová kontrola.
- SQLite a souběh: jeden zapisovatel; `BEGIN IMMEDIATE`, `busy_timeout` 5 s, test 100 souběžných claimů.
- Testy s časem (lease 120 s, náhrobky 7 dní): injektované hodiny ve store a plánovači serveru; žádné `time.Sleep` v testech kromě AK22.
- Měření latence v AK21 je v CI šumové: test asserts bajtovou shodu tvrdě; latenci měří prokládaně (1 000 vzorků × 3 třídy, střídání tříd po jednom vzorku), vypíše medián a p95 každé třídy a selže při mediánu > 3× nebo p95 > 5× nejrychlejší třídy (D2 = A, 22. 10. 2026). Změněno D23 (23. 10. 2026): prahy platí jen mezi třídami se stejnou prací (neznámé id, špatné R nebo M, špatný podpis, klíč mimo křivku), medián nejvýš 1,15× a p95 nejvýš 5× nejrychlejší z nich; prošlá a poškozená výzva nezapisují spotřebu, proto se kontrolují jen na bajtovou shodu. Práh 3× propustil mutant, který pro neznámé id odpověděl o 23 % dřív.
- scrypt N=2^17 v testech (128 MiB, zhruba 0,3 s v Go, jednotky sekund v Node): vektory s profilem 1 jsou dva, zbytek profil 2 nebo bez passphrase.
- PoW ověření na serveru stojí jeden scrypt N=2^14 (zhruba 50 ms): přijatelné jen při zapnutém PoW nad 50 % stropu; CLI a web jej dostanou až v dalším milníku, server ho ověřuje už teď.

## Co zůstává na další milníky

AK2, AK10, AK13, AK16, AK18 až AK20 (web klient), AK14 (CLI), AK15 (reprodukovatelný build), AK17, AK25, AK26 (nasazení), AK24 e2e část, AK12 část s Caddy a Scaleway.

---

# Eng review (/gstack-plan-eng-review, 22. 10. 2026)

Cíl review: plán výše (tento soubor). Design doc: `docs/designs/nylzo-architecture-research.md` (APPROVED). CEO plán: žádný. Reviewer: Claude (Fable 5.1) v sezení `55030-1792622808-c232e5be`; Codex není nainstalovaný, outside voice běží jako nativní fallback.

## Scope Challenge

### A. Posouzení cíle

Co už sub-problémy řeší:
- `tools/r13/echo/main.go:16-80`: vzor `http.Server` s timeouty (ReadHeaderTimeout 5 s, ReadTimeout 120 s, WriteTimeout 120 s, IdleTimeout 30 s, MaxHeaderBytes 8 KiB), h2c přes `http.Protocols`, graceful shutdown, routování Go 1.22 vzory. Server `nylzod` tento vzor převezme (stejné konstanty podle design docu řádek 624), ne kód: echo je měřicí nástroj mimo modul.
- Go stdlib (příloha H): `crypto/hkdf`, `crypto/pbkdf2`, `crypto/aes` + `crypto/cipher` GCM, `crypto/hmac`, `crypto/sha256`, `crypto/subtle`, `crypto/rand`, `net/netip`, `log/slog`, `net/http` [Layer 1]. Jen scrypt z `golang.org/x/crypto` v0.58.0 a SQLite z `modernc.org/sqlite` v1.60.1 [Layer 1, verze z design docu řádek 313-314].
- Web Crypto (příloha A): AES-GCM, HKDF, PBKDF2, `getRandomValues`; `@noble/hashes` 2.4.0 jen pro scrypt [Layer 1].
- Nic z milníku v repozitáři neexistuje: `git log` má dva commity (`c6af35a`, `95b5d1a`), oba jen docs, tools a `.gitignore`.

Minimální změny: plán nepřidává nic nad rámec zadání vlastníka; jediné sporné položky jsou serverové ověření PoW (klient ho v tomto milníku nemá) a měření latence v CI. Obojí zadání výslovně požaduje, PoW zůstává; latence se řeší v nálezu S1.

Složitost (odhad, ne měření): zhruba 45 nových souborů, 7 nových balíčků nebo služeb (core, pepper, store, store/sqlite, storetest, server, TS core) plus 2 příkazy. Práh 8 souborů a 2 služeb je překročen, otázka B se pokládá.

Search check: Aside není nainstalovaný (`NEEDS_ASIDE: Darwin`), WebSearch v tomto sezení není dostupný; postupuji z doložených příloh A, E, H (otevřené zdroje k 21. 10. 2026), ne z vyhledávání. Nové vzory: atomický claim v SQLite (`BEGIN IMMEDIATE`, dokumentace SQLite, příloha E/H neobsahují; vzor je standardní [Layer 1]); token bucket v paměti (stdlib `golang.org/x/time/rate` není stdlib, implementace 30 řádků ručně [Layer 1]).

TODOS.md neexistuje. Completeness: plán jde na plné pokrytí testy včetně souběhu a pádu. Distribution: žádný nový artefakt k publikaci (image a binárky jsou mimo rozsah, uvedeno v „Mimo rozsah“).

### Reality rows

REALITY: premise-table pass 9 premis, 0 unverified; KDF profily a odvození docs/designs/nylzo-architecture-research.md:490-498; obálka a padding docs/designs/nylzo-architecture-research.md:510-521; lease docs/designs/nylzo-architecture-research.md:573-576 (120 s); limity kyblíků a token bucket docs/designs/nylzo-architecture-research.md:619-621; timeouty a Content-Length docs/designs/nylzo-architecture-research.md:624; datový model docs/designs/nylzo-architecture-research.md:639-653; verze Go/x/crypto/sqlite/TS/noble/vitest :310-329 (registry 22. 10. 2026 potvrdil typescript 7.0.2, @noble/hashes 2.4.0, vitest 5.0.5, pnpm 12.14.0 [measured: npm view]); Go 1.27.3 lokálně přes mise [measured: go version]; modernc.org/sqlite v1.61.0 existuje nověji než doložená v1.60.1 [measured: go list -m -versions], plán pinuje doloženou v1.60.1 docs/designs/nylzo-architecture-research.md:314
REALITY: already-done pass origin/main má jen docs, tools/r13 a .gitignore; žádný kód milníku c6af35a
REALITY: surface-check pass HTTP cesty podle tabulky API docs/designs/nylzo-architecture-research.md:657-668; `go` 1.27.3 a `pnpm` 10.33.2 lokálně ověřeny [measured: go version, pnpm --version]; `nylzod sweep-once` a `go run ./cmd/nylzo-vectors` jsou budoucí příkazy (not available), definuje je tento plán docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:17-33
REALITY: binding-decisions pass rozhodnutí vlastníka z 22. 10. (modul github.com/misaon/nylzo, MIT, monorepo) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:5; rozhodnutí větve z 21. 10. (Go server, TS jen web, scrypt profil, sůl z S, kdf_id z fragmentu) docs/designs/nylzo-architecture-research.md:69-70,481,493; žádný výchozí stav je nepřepisuje
REALITY: numbers pass všechna čísla plánu jsou citace z design docu (4 KiB/64 KiB :520, 120 s :573, 50/s burst 100 :621, 1 000 řádků :649, 128 MiB :249) nebo označené [estimated] (0,3 s scrypt v Go, 50 ms PoW, 3× práh latence, 45 souborů) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:72-78
REALITY: deferred-asks pass zadání vlastníka (6 bodů) je pokryto celé; žádný linked issue; vynechané AK jsou vyjmenované docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:82-84
REALITY: pr-count pass jeden PR proti main, .gstack/ship-policy.json neexistuje (výchozí jeden PR na vlnu) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:9
REALITY: transaction-semantics pass claim, čítače a zánik řádku v jedné transakci BEGIN IMMEDIATE, busy_timeout, bez poolu více zapisovatelů; test pádu mezi ReleaseIfDone a Blobs.Delete docs/designs/nylzo-architecture-research.md:570-574,620,653
REALITY: prior-art pass žádná vendorovaná ani patchovaná závislost; modernc.org/sqlite, x/crypto a @noble/hashes se berou upstream v doložených verzích docs/designs/nylzo-architecture-research.md:313-322
REALITY: contract-assumptions pass žádný předpoklad z jiného repozitáře (jediný externí kontrakt je HTTP API definované tímto plánem a design docem) docs/designs/nylzo-architecture-research.md:657-668
REALITY: dependency-fallback pass žádný stacked PR; milník je jeden PR bez předchůdce a bez závislosti na R13 (adaptér postgres+s3 je mimo rozsah) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:9-11
REALITY: obligation-tiers pass must-ship = body 1 až 6 zadání; deferred = AK pro web, CLI, nasazení docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:82-84

### B. Složitost (gate)

Složitost přesahuje práh (45 souborů, 7 balíčků). Navržené škrty funkcí: žádné; serverové ověření PoW a endpoint zneužití zadání vyžaduje a oba jsou malé (odhad: do 150 řádků každý). Otázka D1 řeší jen uspořádání balíčků.

Scope record (22. 10. 2026): feature answers: žádné škrty navrženy, žádné přijaty; structure: A (D1 = „A) Původní“, původní uspořádání 9 modulů); accepted scope: celý plán kroků 1 až 12 beze změny; pending remedies: R1 (do odpovědi D2 níže). Výsledek Scope Challenge: scope accepted as-is.

### C. Nálezy Scope Challenge

1. S1 [P2] (confidence: 8/10) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:76 — Plán navrhuje práh „medián tříd se neliší víc než 3×“ pro latenci AK21 v CI bez prokládání vzorků a bez p95; sdílené runnery GitHubu kolísají a neprokládané série měří drift runneru místo rozdílu tříd. Zdroj: design doc AK21 (:767) chce medián i p95 při 1 000 vzorcích. Řeší R1 (D2).
2. S2 [P3] (confidence: 9/10) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:12 — Plán mlčí o tom, že modernc.org/sqlite má od 21. 10. novější v1.61.0 než doložená v1.60.1 [measured: go list -m -versions]. Faktická korekce, bez otázky: pinuje se doložená v1.60.1 (design doc :314), stejně jako x/crypto v0.58.0; aktualizace verzí je samostatná změna s vlastní kontrolou.
3. S3 [P3] (confidence: 9/10) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:68 — Lokální pnpm je 10.33.2, design doc předepisuje pnpm 12 (:324). Faktická korekce, bez otázky: `packageManager: pnpm@12.14.0` v `web/core/package.json` a pnpm 10 se na tuto verzi sám přepne (`managePackageManagerVersions`); CI používá tutéž hodnotu.

## Decision ledger

### R1: práh latence AK21 v CI
Finding: S1, P2, confidence 8/10, docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:76, reviewer Claude
Plan baseline: původní návrh plánu: „medián tříd se neliší víc než 3×“, bez prokládání a bez p95; bajtová shoda 404/409 je schválená design docem AK21 (:767) a není předmětem volby
Runtime evidence: žádný kód neexistuje; šum sdílených runnerů není v tomto sezení měřen (unknown)
Comparison grid:
| Volba | Současný stav | A | B | C |
|---|---|---|---|---|
| R1 vyhodnocení latence v CI | práh 3× mediánu, neprokládané (plán :76) | prokládané 1 000 × 3 třídy, selhání při mediánu > 3× nebo p95 > 5× nejrychlejší třídy, výpis čísel | žádný práh v CI; měření jen za NYLZO_TIMING_TESTS=1 s výpisem | Mann-Whitney U nad vzorky, selhání při p < 0,01 |
| bajtová shoda 404 a 409 | tvrdá assert (AK21 :767, schváleno) | beze změny | beze změny | beze změny |
| 1 000 vzorků na třídu | ano (AK21 :767, schváleno) | beze změny | beze změny, jen mimo CI | beze změny |
Question D2:
D2 — Jak vyhodnotit latenci 404 (AK21) v CI?
Project/branch/task: nylzo, větev claude/gracious-chebyshev-79747d, test serveru AK21.
ELI10: AK21 chce, aby 404 pro neznámé id, špatné R a špatné M byly bajtově stejné a časově k nerozeznání. Bajtová shoda je deterministická. Časová shoda se měří 1 000 vzorky na třídu; na sdílených CI runnerech kolísá, takže tvrdý práh by někdy shodil CI bez chyby v kódu. Volba je, jak přísně v CI vyhodnotit čas.
Stakes if we pick wrong: Příliš přísně = falešně červené CI a lidé test začnou ignorovat; příliš volně = regres v uniformní práci (třeba chybějící dummy HMAC při 404) projde bez povšimnutí.
Recommendation: A because prokládané vzorkování ruší drift runneru a práh 3× mediánu je daleko nad šumem, ale pod rozdílem „jeden HMAC a dotaz navíc proti ničemu“ (odhad: HMAC plus dotaz SQLite zhruba 50 až 200 µs proti holé odpovědi).
Completeness: A=9/10, B=6/10, C=10/10
Pros / cons:
A) Práh v CI, prokládaně (recommended)
  ✅ Hlídá regres v uniformní práci serveru při každém běhu CI, s prahem, který šum sdílených runnerů nepřekročí
  ✅ Medián a p95 jsou v logu CI, takže tvrzení stránky transparentnosti má doložitelné číslo
  ❌ Zbytkové riziko falešně červeného běhu při extrémním zatížení runneru (odhad: vzácné, běh jde zopakovat)
B) Jen bajty v CI, čas mimo CI
  ✅ CI nikdy nespadne kvůli šumu runneru
  ✅ Měření zůstává v repu a dá se spustit ručně před vydáním
  ❌ Regres v uniformní práci v CI nikdo neuvidí; měření musí někdo spustit a číst ručně
C) Statistický test v CI
  ✅ Zachytí systematický rozdíl i hluboko pod 3×
  ❌ Vlastní statistika v testu (desítky řádků cizí domény) a při 1 000 vzorcích zachytí i rozdíly v jednotkách µs, které vyrobí runner sám (falešná pozitiva)
Net: A dává reálnou kontrolu s malým rizikem šumu; B je pro CI bezpečná, ale AK21 pak v CI nehlídá; C je přesnější, ale přidává kód a falešná pozitiva.
Header: Latence AK21 v CI
Options:
A) Práh v CI, prokládaně (recommended)
Prokládané vzorkování 1 000 × 3 třídy; selhání při mediánu > 3× nebo p95 > 5× nejrychlejší třídy; výpis mediánu a p95 do logu. ✅ Hlídá regres v uniformní práci při každém běhu CI. ✅ Čísla v logu CI pro stránku transparentnosti. ❌ Vzácné riziko falešně červeného běhu při extrémním zatížení runneru.
B) Jen bajty v CI, čas mimo CI
Bajtová shoda tvrdě v CI; měření latence jen za NYLZO_TIMING_TESTS=1 bez prahu, jen výpis. ✅ CI nikdy nespadne kvůli šumu. ✅ Měření zůstává v repu pro ruční běh před vydáním. ❌ Regres v uniformní práci v CI nikdo neuvidí.
C) Statistický test v CI
Mann-Whitney U nad vzorky, selhání při p < 0,01. ✅ Zachytí systematický rozdíl i pod 3×. ❌ Desítky řádků vlastní statistiky a falešná pozitiva z rozdílů v jednotkách µs, které vyrobí runner.

State: approved
Actual answer: A) Práh v CI, prokládaně (D2, odpověď vlastníka 22. 10. 2026)
Accepted scope: test AK21 v internal/server: bajtová shoda 404 (neznámé id, špatné R, špatné M) a 409 (živé id, náhrobek) tvrdě; latence 1 000 vzorků × 3 třídy prokládaně (střídání tříd po jednom vzorku), medián a p95 každé třídy do logu testu, selhání při mediánu > 3× nebo p95 > 5× nejrychlejší třídy; běží v CI v jobu go
History: původní návrh plánu „medián tříd se neliší víc než 3×“ bez prokládání a p95 (plán :76), nahrazen D2 = A

Approval readiness: PASS. Zkontrolované ID: R1 (approved, D2 = A, odpověď vlastníka 22. 10. 2026). Nálezy A1, A2, A4, A5, A6, Q1 až Q4, P1 až P4 a testy sekce 3 jsou nutná implementace a důkaz chování schváleného design docem (citace u každého nálezu) a zadáním vlastníka; A3 a P5 nemění nic. Žádná odložená volba. Outside voice: unavailable (viz níže), do ledgeru nic nepřidal.

## 1. Architecture review

Hranice: klientské jádro (`internal/core`, `web/core`) drží S, K, P; server (`internal/server`, `internal/pepper`, `internal/store`) drží jen ciphertext, HMAC s pepperem a čítače. Jediný sdílený artefakt obou jader je `spec/vectors.json`. Jediný stavový prvek je SQLite soubor plus adresář blobů (jeden zapisovatel, WAL); žádná replika, žádná záloha obsahu (OD20).

Životní cyklus zprávy (do `spec/protocol.md` jako diagram):

```
 POST create ──▶ available ──claim (opens_remaining>0)──▶ available | opened (opens_remaining==0)
                    │   ▲                                      │
                    │   └── lease L (120 s) ─ GET blob ≤3× ─ ack ┘
                    │                                          │
   expires_at < now │ burn (R) │ cancel (M)      poslední lease skončil nebo ack
                    ▼          ▼                               ▼
              náhrobek {id, mac(h(R)), mac(h(M)), state, first_opened_at, ended_at, purge_at = +7 d}
                                 │ sweep po purge_at
                                 ▼
                              smazáno (id neznámé → 404)
```

Nálezy:

1. A1 [P2] (confidence: 8/10) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:53 — Plán říká „kontrola Content-Length před čtením těla“, ale neříká, kam tělo teče. Načtení celého těla do paměti (až 5 MiB + 100 KiB + 64 KiB) při souběhu 80 požadavků (limit Scaleway, design doc :459) dá přes 400 MiB, víc než 256 MB kontejneru (design doc :459). Remedy (implementace schválené věty „uloží blob a metadata“, design doc :545, a `MaxBytesReader` :624, bez nové volby): tělo se streamuje přes `MaxBytesReader` do dočasného souboru v `data/blobs/tmp/`, po `fsync` se vloží řádek `messages` a soubor se přejmenuje na `data/blobs/{id}`; selhání vložení dočasný soubor smaže. Server při tom přečte prvních 15 bajtů hlavičky obálky, uloží `version` (design doc :629 „Server ukládá verzi v metadatech“) a odmítne 400, když `kdf_id` v hlavičce obálky nesouhlasí s `kdf_id` v `X-Nylzo-Create`. Disposition: accepted (exact prior approval design doc :545, :624, :629).
2. A2 [P2] (confidence: 9/10) docs/designs/nylzo-architecture-research.md:485 — `pepper_id (1 B)` je v řádku, ale doc neříká, jak id vzniká; bez pravidla by operátor při rotaci musel spravovat čísla ručně a záměna by znehodnotila ověření. Remedy: `pepper_id = SHA-256("nylzo/v1/pepper-id" || pepper)[0]`; server při startu odmítne konfiguraci, kde aktuální a předchozí pepper dávají stejné id (pravděpodobnost 1/256, hlášeno jasně bez hodnot pepperu). Zapsat do `spec/protocol.md` jako serverový detail. Disposition: accepted (implementace schválené rotace, design doc :485 a AK27 :773; žádná nová volba chování).
3. A3 [P2] (confidence: 8/10) docs/designs/nylzo-architecture-research.md:621 — Token bucket 50/s per instance je globální, takže jeden zdroj záplavy vyčerpá kyblík všem klientům instance. Doc to přijímá jako ochranu databáze a rozpočtu před dostupností (recenze zneužití F2). Disposition: accepted as documented risk; žádná změna. Realistické selhání v produkci: útočník s rotujícími adresami drží kyblík prázdný, legitimní čtenáři dostávají 429 s `Retry-After`; test AK12/AK22 to měří, uživatel vidí jasnou chybu, ne tiché selhání.
4. A4 [P3] (confidence: 9/10) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:53 — Webový klient v milníku není, ale CSP nonce a `Sec-Purpose` 503 pro `/m/*` vyžadují HTML odpověď. Remedy: server servíruje vestavěnou minimální šablonu `index.html` (bez skriptu, s `{{nonce}}` placeholderem na `<script>`/`<link>`, které klient vlastníka později doplní) pro `/` a `/m/{id}`; stejné hlavičky jako pro budoucí klient. Disposition: accepted (nutná infrastruktura pro schválené hlavičky, design doc :608-609).
5. A5 [P2] (confidence: 9/10) docs/designs/nylzo-architecture-research.md:462 — `nylzod sweep-once` pepper nepotřebuje („úloha pepper nepotřebuje“). Konfigurace proto `NYLZO_PEPPER` vyžaduje jen pro `serve`; `sweep-once` běží bez něj. Disposition: accepted (exact prior approval design doc :462).
6. A6 [P3] (confidence: 7/10) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:50 — Formát PoW tokenu (OD24 fixuje algoritmus, ne kódování): `challenge = base64url(window_start 4 B BE || nonce 16 B || HMAC-SHA-256(k_pow, window_start || nonce)[0:16])` vydaný v `GET /api/v1/instance` (`pow.challenge`), řešení `X-Nylzo-Pow: challenge.counter` kde `scrypt(N=2^14, r=8, p=1, password = nonce || id, salt = counter 8 B BE, 32)` má prvních `difficulty` bitů nulových (výchozí 4, konfigurace `NYLZO_POW_DIFFICULTY`); platnost `now − window_start ≤ 120 s`. Server ověří jedním scryptem (odhad 50 ms, 16 MiB). Jde o detail nad OD24, PR ho označí. Disposition: accepted as implementation detail of OD24 (design doc :151, :623); klient (CLI) ho dostane v dalším milníku, formát se do vydání v1 může ještě změnit a spec to říká.

Distribution architecture: žádný nový publikovaný artefakt (binárky, image, podpisy jsou mimo rozsah podle zadání); CI jen testuje. Disposition: deferred per brief.

Dispositions: A1 accepted (prior approval), A2 accepted (prior approval), A3 accepted as documented risk, A4 accepted (prior approval), A5 accepted (prior approval), A6 accepted as OD24 detail.

## 2. Code quality review

1. Q1 [P2] (confidence: 9/10) docs/designs/nylzo-architecture-research.md:545 — „přesně 22 znaků base64url“ nestačí: 22 znaků base64url kóduje 132 bitů, takže poslední znak nese jen 2 datové bity a jeho spodní 4 bity musí být nulové (jen `A Q g w`); jinak dva různé řetězce (`…A` a `…B`) znamenají stejných 16 bajtů, ale v DB (PK text) dva řádky, a klient by je odvozoval stejně. Totéž pro S (43 znaků, 258 bitů, poslední znak s 2 nulovými bity, tedy jen `A E I M Q U Y c g k o s w 0 4 8`); kanonický tvar S proto nikdy nekončí `-` ani `_`, pravidlo design docu :480 je splněno konstrukcí. Remedy: obě jádra i server přijmou jen kanonický tvar (dekódovat, zakódovat zpět, porovnat); vektory obsahují záporné případy. Disposition: accepted (nutná korektnost schváleného OD12, design doc :139).
2. Q2 [P2] (confidence: 8/10) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:44 — `DecodeMeta` musí mít explicitní hranice: `v == 1`, aspoň jedno z `text`/`file`, `text` do 100 KiB UTF-8 (OD2), `file.name` 1 až 255 bajtů bez `/`, `\`, NUL a řídicích znaků (klient čistí před zobrazením, doc :519, ale jádro odmítne zjevně vadný název), `file.mime` do 255 bajtů, `file.size` rovné délce bajtů souboru v plaintextu, `len_meta` do 128 KiB; neznámé klíče se ignorují (dopředná kompatibilita v rámci v1). Disposition: accepted (edge cases schváleného formátu, design doc :519-521).
3. Q3 [P2] (confidence: 8/10) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:53 — Chyby úložiště musí mít typované hodnoty (`ErrNotFound`, `ErrGone{state}`, `ErrLeaseInvalid`, `ErrPassphrase{retryAfter, attemptsLeft}`, `ErrRateLimited{retryAfter}`), které handler mapuje na jednotné JSON odpovědi; jiná chyba → 500 bez detailu v těle a log bez hodnot požadavku. Disposition: accepted (schválená tabulka chyb, design doc :670).
4. Q4 [P3] (confidence: 8/10) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:53 — `X-Nylzo-Create` se dekóduje striktně (`DisallowUnknownFields`, délka hlavičky do 1 KiB před base64 dekódováním, `size` do limitu, `opens_max` 1 až limit, `ttl_s` 60 až limit, `kdf_id` 0 až 2, `verifier_hash` právě tehdy, když `kdf_id != 0`). Disposition: accepted (schválené pořadí kontrol, design doc :545).
5. Q5 [P3] (confidence: 9/10) — Sdílený kód: dvě jádra jsou záměrně duplicitní (jazyky, design doc :22) a sdílený kontrakt jsou vektory; v Go je jediný kandidát na sdílení `base64url` kanonizace mezi `core` a `server` (server importuje `core`, žádná extrakce). Žádný další helper není doložený dvěma volajícími. Disposition: no extraction.

Diagramy: datový tok v plánu (řádky 36-52) platí; doplnit stavový diagram (sekce 1) do `spec/protocol.md`.

Dispositions: Q1 accepted, Q2 accepted, Q3 accepted, Q4 accepted, Q5 no extraction.

## 3. Test review

Framework: Go `testing` (`go test ./...`, fuzz přes `go test -fuzz`), TS Vitest 5.0.5 (`pnpm --filter @nylzo/core test`); `go.mod` ani `package.json` zatím neexistují (plán je zakládá), takže detekce vychází z design docu :325 a zadání.

Krok 1 až 3 (trasování, uživatelské toky, pokrytí): žádný kód neexistuje, všechny cesty jsou GAP a plán je pokrývá navrženými testy; „uživatel“ milníku je HTTP klient (CLI nebo web později), proto jsou toky vyjádřené jako HTTP sekvence.

```
CODE PATHS (navržené)                                              USER FLOWS (HTTP)
[+] internal/core                                                  [+] Vytvoření → odkaz
  ├── Derive (hsalt, R, salt_pw, P, K, V)                           ├── [GAP] [→E2E] create 201 → GET meta 200 (AK1, AK3)
  │   ├── [GAP] kdf 0/1/2 proti vektorům (NFC, diakritika)          ├── [GAP] create bez Content-Length 411, size≠CL 400, >limit 413 (AK22)
  │   └── [GAP] RFC 7914 vektory 1-4                                ├── [GAP] 21. create z IP za hodinu 429 (AK12); /64 a /48 kyblíky (AK23)
  ├── Seal/Open                                                     ├── [GAP] globální strop 503 jen pro create, claim běží (AK12)
  │   ├── [GAP] shoda objektu s vektory (pevný IV)                  └── [GAP] PoW vyžadován při 50 % stropu, špatné řešení 403
  │   ├── [GAP] 1 bit změněn → chyba (AK8)                        [+] Čtení
  │   ├── [GAP] jiná politika v AAD → chyba (AK8)                   ├── [GAP] [→E2E] claim 200 + lease, GET blob ≤3× (429 na 4.), ack → 410 a blob pryč (AK6)
  │   ├── [GAP] přenos pod jiné id → chyba                          ├── [GAP] 2 souběžné claimy opens=1: 200 + 410, 100× (AK4)
  │   ├── [GAP] padding 4 KiB / 64 KiB hranice (0, 1, 4095, 4096 B) ├── [GAP] 3 souběžné claimy opens=3: 3 lease; lease vyprší bez ack → 410 ihned, blob do 130 s (AK6)
  │   └── [GAP] fuzz Open, DecodeMeta, ParseLink                    ├── [GAP] pád mezi ReleaseIfDone a Blobs.Delete → ReconcileBlobs smaže sirotka (AK6)
  ├── ParseLink/FormatLink                                          ├── [GAP] špatná passphrase: 1-5 bez čekání, 6. ≥2 s, 10. ≥32 s, 100/den → 403 všem, status 100 (AK5)
  │   ├── [GAP] kanonický base64url (id, S), odmítnutí `-`/`_` na konci  ├── [GAP] expirace → blob smazán sweepem, meta expired, po 7 d 404 (AK7)
  │   └── [GAP] kdf_id jen z fragmentu; assertProfile (AK24)         └── [GAP] burn (R) a cancel (M) → 410 state, blob pryč
[+] internal/pepper                                               [+] Nerozlišitelnost
  ├── [GAP] mac per účel, Equal konst. čas, Dummy stejná délka       ├── [GAP] 404 neznámé id = špatné R = špatné M bajtově; 409 živé = náhrobek (AK21)
  ├── [GAP] Bucket /32 /24 /64 /48 (kanonizace ::ffff:)             └── [GAP] latence 1 000 × 3 třídy prokládaně, medián/p95, práh 3×/5× (D2)
  └── [GAP] Ring: ověří starým, zapíše novým, bez starého (AK27)   [+] Provoz
[+] internal/store/sqlite (+ storetest)                              ├── [GAP] 200 pomalých spojení, ostatní do 2 s (AK22)
  ├── [GAP] Create/Get/Claim/CheckLease/Ack/ReleaseIfDone/ClearBlob  ├── [GAP] log bez S, R, V, M, Authorization, IP (AK11)
  ├── [GAP] Burn/Cancel/Status/Sweep/ReconcileBlobs/RateHit          ├── [GAP] hlavičky CSP (jiný nonce), Referrer-Policy, no-store, X-Robots-Tag, HSTS, COOP na každé odpovědi
  ├── [GAP] PassphraseFail uvnitř Claim (OD13)                       ├── [GAP] Sec-Purpose → 503 pro /m/* a /api/*; náhledové UA → HTML bez skriptu
  └── [GAP] dvojí Delete, chybějící soubor = úspěch                  ├── [GAP] X-Forwarded-For za důvěryhodnou proxy; podvržené z nedůvěryhodné ignorováno (AK12)
[+] internal/server                                                  ├── [GAP] sweep-once bez NYLZO_PEPPER projde; serve bez něj odmítne start
  ├── [GAP] handlery × stavové kódy tabulky API (400/403/404/409/410/413/429/503)  └── [GAP] fuzz X-Nylzo-Create
  ├── [GAP] client IP (trusted proxies, scaleway mode, žádná konfigurace)
  └── [GAP] token bucket 50/s burst 100
[+] web/core (Vitest)
  ├── [GAP] stejné vektory: derive, kdf (RFC 7914, 4. za NYLZO_HEAVY_TESTS=1 v CI zapnuto), seal/open bajt po bajtu, link
  ├── [GAP] K neextrahovatelný (exportKey vyhodí), V/R přes deriveBits
  └── [GAP] AK8 a AK24 (profil ze serveru ≠ fragment → žádný ověřovač se neodvodí)

COVERAGE: 0/48 paths tested (0%)  |  Code paths: 0/28 (0%)  |  User flows: 0/20 (0%)
QUALITY: ★★★:0 ★★:0 ★:0  |  GAPS: 48 (4 E2E přes httptest, 0 eval)
```

Legend: ★★★ behavior + edge + error  |  ★★ happy path  |  ★ smoke check
[→E2E] = needs integration test  |  [→EVAL] = needs LLM eval

LLM/eval scope: žádný.

Regression rule: žádný existující kód ani chování; riziko regrese nevzniká.

Krok 5, testy k doplnění (všechny plynou ze zadání vlastníka a AK, tedy exact prior approval; nový je jen D2 práh a gating RFC vektoru 4, které nesnižují pokrytí v CI):
- `internal/core/*_test.go`: tabulkové testy nad `spec/vectors.json` (Value: protects=obě jádra dávají stejné bajty; fails_when=změna odvození, paddingu, kódování meta; why_new=žádné testy; seam=none; no_claim=správnost návrhu KDF, jen shoda s vektory); AK8 testy (Value: protects=integrita AAD; fails_when=AAD bez politiky nebo id; why_new=žádné; seam=none; no_claim=nic nad GCM); `FuzzOpen`, `FuzzDecodeMeta`, `FuzzParseLink`, `FuzzParseCreateHeader` (Value: protects=žádný panic a žádné přijetí vadného vstupu; fails_when=index mimo rozsah, neomezená alokace; why_new=žádné; seam=none; no_claim=úplnost, fuzz je vzorkování); `TestVectorsUpToDate` (regenerace == soubor).
- `internal/pepper/*_test.go`: mac per účel liší se, Equal, Dummy, Bucket (IPv4 /32 a /24, IPv6 /64 a /48, `::ffff:` mapování, rotace o půlnoci UTC), Ring AK27.
- `internal/store/storetest`: sdílená sada AK3, AK4 (100×), AK5, AK6 (včetně pádu a dvojího Delete), AK7, Sweep dávky, RateHit atomicita (Value na každou: protects=invarianty úložiště podle datového modelu; fails_when=ztráta transakce, špatný ReleaseIfDone; why_new=žádné; seam=Clock injekce (potřebná i produkčně pro lease podle hodin procesu); no_claim=chování Postgres adaptéru).
- `internal/server/*_test.go`: AK1, AK3, AK4, AK5, AK6, AK11, AK12, AK21 (D2), AK22, AK23, hlavičky, Sec-Purpose, UA náhledů, client IP, token bucket, PoW, sweep-once bez pepperu.
- `web/core/test/*.test.ts`: vektory (derive, kdf, envelope bajt po bajtu, link), AK8, AK24, neextrahovatelný K, RFC 7914 (vektor 4 za `NYLZO_HEAVY_TESTS=1`, CI ho zapíná).

Tests made obsolete by this plan: žádné (žádné existují).

Dispositions: všechny testy accepted jako nutný důkaz schválených AK (zadání vlastníka, design doc :747-773) a D2; žádná nová otázka.

## 4. Performance review

1. P1 [P2] (confidence: 8/10) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:45-47 — Sweep a lazy úklid procházejí tabulky podle času; bez indexů jde o full scan při každém požadavku (ReleaseIfDone per id je PK lookup, ale sweep ne). Remedy: indexy `messages(expires_at)`, `messages(opens_remaining, blob_ref)`, `leases(id)`, `leases(lease_expires_at)`, `tombstones(purge_at)`, `rate_counters(purge_at)`, `passphrase_attempts(purge_at)`; mazání v dávkách po 1 000 (design doc :649). Měřítko: tisíce řádků na instanci (design doc odhad 200 zpráv denně :464), takže jde o hygienu, ne o naměřený problém. Disposition: accepted (schválený datový model :639-649).
2. P2 [P2] (confidence: 8/10) — Paměť při vytvoření: viz A1 (streamování do souboru); čtení bloba přes `http.ServeContent`/`io.Copy` ze souboru, ne do paměti. Disposition: accepted (A1).
3. P3 [P3] (confidence: 9/10) docs/plans/2026-10-22-milestone-1-protocol-cores-server.md:77 — scrypt N=2^17 (128 MiB, odhad 0,3 s v Go a jednotky sekund v Node) v testech: plán drží dva vektory profilu 1; RFC 7914 vektor 4 (1 GiB) v Go vždy, v TS za `NYLZO_HEAVY_TESTS=1` (CI zapnuto). Disposition: accepted.
4. P4 [P3] (confidence: 7/10) — PoW ověření stojí server jeden scrypt N=2^14 (16 MiB, odhad 50 ms) na vytvoření, jen nad 50 % stropu; při 80 souběžných vytvořeních 1,3 GiB paměti. Remedy: semafor na ověření PoW (výchozí 4 souběžná, `NYLZO_POW_CONCURRENCY`), nad ním 503 s `Retry-After`; ověření až po kontrole Content-Length a čítačů (pořadí z design docu :545). Disposition: accepted (ochrana schváleného OD24 před vlastním vyčerpáním; žádná nová volba chování pro klienta).
5. P5 [P3] (confidence: 9/10) — Čítače: create = až 5 atomických upsertů (h/32, d/32, h/24, d/24 s bytes, global); claim/meta/blob = 1; SQLite v jedné transakci; při 50/s stropu token bucketu zanedbatelné. Disposition: no change.

Dispositions: P1 accepted, P2 accepted (A1), P3 accepted, P4 accepted, P5 no change.

## Outside voice

Codex CLI není nainstalovaný (`CODEX_MODE: not_installed`). Nativní fallback (Plan subagent, limit 5 minut) nedodal kompletní zprávu do vypršení časovače; agent byl zastaven, výsledek se nepočítá. Záznam: `codex-plan-review` status `unavailable`, `outside_status: unavailable`. Fix pro příště: `codex login`, nebo delší limit pro nativní fallback. Cross-model tension se nekoná.

## TODOS.md

Žádný nový TODO; následné milníky (web, CLI, postgres+s3, nasazení) jsou v design docu „Next Steps“ a v sekci „Co zůstává na další milníky“ výše.

## NOT in scope

- Webový klient a UI: dělá vlastník (zadání).
- CLI `nylzo`, MCP server, skill: další milníky (design doc Next Steps 6, 10).
- Adaptér postgres+s3: čeká na výsledek testu R13 (design doc R13, OD21).
- Dockerfile, image, podpisy, provenance, nasazení: mimo zadání milníku.
- Mann-Whitney statistika pro AK21: zamítnuto v D2 (práh 3×/5× stačí, méně kódu).
- Aktualizace modernc.org/sqlite na v1.61.0: samostatná změna s vlastní kontrolou (S2).

## What already exists

- `tools/r13/echo/main.go` (vzor timeoutů, h2c, graceful shutdown): převzetí vzoru, ne kódu; echo je mimo modul a zůstává měřicím nástrojem.
- Go stdlib `crypto/hkdf` (`Extract`, `Expand`), `crypto/pbkdf2.Key`, `crypto/cipher` GCM, `crypto/hmac`, `crypto/subtle`, `net/netip`, `log/slog`, `net/http` Go 1.22 routování [Layer 1], ověřeno `go doc` na go1.27.3.
- `golang.org/x/crypto/scrypt` v0.58.0, `modernc.org/sqlite` v1.60.1 (DSN `_txlock=immediate`, `_busy_timeout`, `_journal_mode`, `_pragma`, ověřeno ve zdroji modulu) [Layer 1].
- Web Crypto AES-GCM, HKDF, PBKDF2 a `@noble/hashes/scrypt.js` 2.4.0 (`scrypt(password, salt, {N, r, p, dkLen, maxmem})`, výchozí `maxmem` 1 GiB + 2 KiB pokrývá i RFC vektor 4) [Layer 1]; TypeScript 7.0.2 nativní `tsc` a Vitest 5.0.5 ověřeny instalací přes pnpm 12.14.0 (`packageManager` přepíná lokální pnpm 10.33.2 přes corepack).
- Žádný sdílený helper k extrakci (Q5).

## Failure modes

| Cesta | Realistické selhání | Test | Ošetření | Uživatel vidí |
|---|---|---|---|---|
| Create se streamováním do souboru (A1) | Disk plný nebo pád mezi zápisem souboru a INSERT | AK22 (lživý Content-Length), test pádu v storetest, ReconcileBlobs maže sirotky | 500 bez detailu, dočasný soubor smazán, sirotek do dne pryč | Jasná chyba 500/413, odkaz nevznikl |
| Claim v transakci | Dva souběžné claimy, zámek SQLite (`busy_timeout`) | AK4 100×, AK6 | `BEGIN IMMEDIATE`, retry přes busy_timeout 5 s | Právě jeden 200, druhý 410 |
| Lease a ReleaseIfDone | Instance skončí před časovačem | AK6 (sweep po pádu), AK7 | Sweep a lazy úklid volají ReleaseIfDone znovu | Blob nedostupný ihned (410), smazán do intervalu úklidu |
| Pepper rotace | Operátor nastaví oba peppery se stejným id (1/256) | AK27 + test kolize id | Start odmítne konfiguraci s jasnou hláškou bez hodnot | Server nenastartuje, log říká proč |
| Rate limit za proxy | Chybná `NYLZO_TRUSTED_PROXIES`: všichni sdílí kyblík ingressu | AK12 (podvržená hlavička z nedůvěryhodné adresy) | Hlavičky se bez konfigurace ignorují | 429 pro všechny po 20 vytvořeních (bezpečné pro rozpočet) |
| PoW ověření | Nával vytvoření při zapnutém PoW vyčerpá CPU a paměť | test semaforu (P4) | Semafor 4 souběhů, nad ním 503 s Retry-After | 503 jen pro vytváření, čtení běží |
| Logování | Handler omylem zaloguje hlavičku | AK11 sken logu | Logger dostává jen whitelist polí | nic (chyba se chytí v CI) |

Kritické mezery (bez testu, bez ošetření, tiché): žádné.

## Worktree parallelization strategy

| Step | Modules touched | Depends on |
|------|----------------|------------|
| spec | spec/ | — |
| core | internal/core, cmd/nylzo-vectors, spec/vectors.json | spec |
| pepper | internal/pepper | core (konstanty) |
| store | internal/store, internal/store/sqlite, internal/store/storetest | core (typy id), pepper (mac v testech) |
| ts-core | web/core | core (vectors.json) |
| server | internal/server, cmd/nylzod | core, pepper, store |
| ci-docs | .github/workflows, README.md, LICENSE | všechny |

Parallel lanes: Lane A: spec → core → pepper → store → server; Lane B: ts-core (po vectors.json). Execution order: spec, core a vektory sériově; pak spustit B (TS) paralelně s A (pepper, store, server); ci-docs nakonec. Conflict flags: `spec/vectors.json` je jediný sdílený soubor obou lane; vzniká jen v lane A a lane B ho jen čte. V této session běží obě lane sekvenčně v jednom worktree (jeden implementátor).

## Implementation Tasks
Synthesized from this review's findings. Each task derives from a specific finding above. Run with Claude Code or Codex; checkbox as you ship.

- [ ] **T1 (P1, human: ~1 day / CC: ~20 min)** — internal/server — Streamovat tělo create přes MaxBytesReader do dočasného souboru, peek 15 B hlavičky, verze do metadat, 400 při neshodě kdf_id
  - Surfaced by: Architecture review — A1
  - Files: internal/server/create.go, internal/store/sqlite/blobs.go
  - Verify: `go test ./internal/server -run 'TestCreate|TestAK22'`
- [ ] **T2 (P1, human: ~2 h / CC: ~10 min)** — internal/pepper — pepper_id = SHA-256("nylzo/v1/pepper-id" || pepper)[0]; Ring s aktuálním a předchozím; odmítnutí kolize id při startu
  - Surfaced by: Architecture review — A2
  - Files: internal/pepper/pepper.go, internal/pepper/pepper_test.go, spec/protocol.md
  - Verify: `go test ./internal/pepper` (AK27 v storetest a serveru)
- [ ] **T3 (P2, human: ~2 h / CC: ~10 min)** — internal/server — Vestavěná šablona index.html s nonce pro / a /m/{id}; Sec-Purpose 503; náhledové UA bez skriptu
  - Surfaced by: Architecture review — A4
  - Files: internal/server/static.go, internal/server/templates/index.html
  - Verify: `go test ./internal/server -run 'TestHeaders|TestSecPurpose|TestPreviewAgents'`
- [ ] **T4 (P2, human: ~1 h / CC: ~5 min)** — cmd/nylzod — sweep-once bez NYLZO_PEPPER; serve bez něj odmítne start
  - Surfaced by: Architecture review — A5
  - Files: cmd/nylzod/main.go, internal/server/config.go
  - Verify: `go test ./internal/server -run TestConfig`
- [ ] **T5 (P2, human: ~1 day / CC: ~20 min)** — internal/server — PoW: challenge v /api/v1/instance, X-Nylzo-Pow ověření scryptem N=2^14, difficulty, okno 120 s, semafor souběhu, zapnutí při 50 % stropu
  - Surfaced by: Architecture review — A6; Performance review — P4
  - Files: internal/server/pow.go, internal/pepper/pepper.go (k_pow), spec/protocol.md
  - Verify: `go test ./internal/server -run TestPow`
- [ ] **T6 (P1, human: ~3 h / CC: ~10 min)** — internal/core, web/core — Kanonický base64url pro id (22 znaků, poslední z `A Q g w`) a S (43 znaků, poslední z 16 povolených); záporné vektory
  - Surfaced by: Code quality review — Q1
  - Files: internal/core/base64url.go, web/core/src/base64url.ts, cmd/nylzo-vectors/main.go
  - Verify: `go test ./internal/core -run TestLink`; `pnpm --filter @nylzo/core test`
- [ ] **T7 (P2, human: ~3 h / CC: ~10 min)** — internal/core, web/core — DecodeMeta s hranicemi (v, text ≤ 100 KiB, name 1-255 B bez / \ NUL, mime ≤ 255 B, size = délka souboru, len_meta ≤ 128 KiB); fuzz
  - Surfaced by: Code quality review — Q2
  - Files: internal/core/meta.go, internal/core/meta_test.go, web/core/src/meta.ts
  - Verify: `go test ./internal/core -run 'TestMeta|FuzzDecodeMeta'`
- [ ] **T8 (P2, human: ~2 h / CC: ~10 min)** — internal/store, internal/server — Typované chyby store a jejich mapování na jednotné JSON odpovědi
  - Surfaced by: Code quality review — Q3
  - Files: internal/store/errors.go, internal/server/errors.go
  - Verify: `go test ./internal/server -run TestErrorMapping`
- [ ] **T9 (P2, human: ~1 h / CC: ~5 min)** — internal/server — Striktní parsování X-Nylzo-Create (DisallowUnknownFields, limity polí, verifier_hash právě při kdf_id != 0); fuzz
  - Surfaced by: Code quality review — Q4
  - Files: internal/server/create.go, internal/server/create_fuzz_test.go
  - Verify: `go test ./internal/server -run 'TestCreateHeader|FuzzCreateHeader'`
- [ ] **T10 (P2, human: ~1 h / CC: ~5 min)** — internal/store/sqlite — Indexy expires_at, (opens_remaining, blob_ref), leases(id), leases(lease_expires_at), purge_at na tombstones, rate_counters, passphrase_attempts; sweep v dávkách 1 000
  - Surfaced by: Performance review — P1
  - Files: internal/store/sqlite/schema.sql
  - Verify: `go test ./internal/store/sqlite -run TestSweep`
- [ ] **T11 (P1, human: ~4 h / CC: ~15 min)** — internal/server — AK21: bajtová shoda 404/409, prokládaných 1 000 × 3 třídy, medián/p95 do logu, práh 3× a 5× (D2 = A)
  - Surfaced by: Scope Challenge — S1 / R1
  - Files: internal/server/uniform_test.go
  - Verify: `go test ./internal/server -run TestAK21 -v`
- [ ] **T12 (P2, human: ~30 min / CC: ~3 min)** — go.mod, web/core/package.json — Pin modernc.org/sqlite v1.60.1, x/crypto v0.58.0, packageManager pnpm@12.14.0, typescript 7.0.2, vitest 5.0.5, @noble/hashes 2.4.0
  - Surfaced by: Scope Challenge — S2, S3
  - Files: go.mod, go.sum, web/core/package.json, pnpm-lock.yaml, pnpm-workspace.yaml
  - Verify: `go mod verify`; `pnpm install --frozen-lockfile`
- [ ] **T13 (P2, human: ~1 h / CC: ~5 min)** — web/core — RFC 7914 vektor 4 za NYLZO_HEAVY_TESTS=1; CI ho zapíná
  - Surfaced by: Performance review — P3
  - Files: web/core/test/kdf.test.ts, .github/workflows/ci.yml
  - Verify: `NYLZO_HEAVY_TESTS=1 pnpm --filter @nylzo/core test`

_No new tasks from Test review beyond the AK suite listed in section 3 (tests are part of each task's Verify)._

## Unresolved decisions that may bite you later

Žádné. Všechna rozhodnutí tohoto review mají odpověď (D1 = A, D2 = A) nebo exact prior approval.

## Completion summary

- Step 0: Scope Challenge — scope accepted as-is
- Reality rows: 12/12 applicable rows emitted (`PLAN_REALITY: phase=eng rows=12/12 missing=none verdict=complete`), 0 unverified premises
- Architecture Review: 6 issues found
- Code Quality Review: 5 issues found
- Test Review: diagram produced, 48 gaps identified (vše nový kód; každá mezera má navržený test v sekci 3)
- Performance Review: 5 issues found
- NOT in scope: written
- What already exists: written
- TODOS.md updates: 0 items proposed to user
- Failure modes: 0 critical gaps flagged
- Unresolved decisions: 0 in this review
- Outside voice: codex, unavailable (CLI není nainstalovaný; nativní fallback nestihl limit 5 minut)
- Parallelization: 2 lanes, 1 parallel (ts-core) / 6 sequential
- Navigation: C) Ready to implement, zvoleno automaticky (vlastník 22. 10. 2026 požádal o samostatnou práci do rána); doporučený implementační model podle gstack-models se nezjišťoval
- Lake Score: 1/1 = D2 vybral A (9/10, nejúplnější praktická volba; C 10/10 zamítnuto kvůli falešným pozitivům); D1 bez skóre (liší se druhem)

## Suppressed findings (appendix)

- [P3] (confidence: 4/10) web/core — Web Crypto v Node 24 a v prohlížečích může lišit chybu při `deriveKey` s `extractable=false` a následném `exportKey`; test jen ověří, že export selže. Nepotvrzeno čtením zdroje, proto potlačeno.
- [P3] (confidence: 3/10) internal/store/sqlite — `database/sql` pool s více spojeními a `_txlock=immediate` může při čtení držet zbytečné zámky; plán drží jedno zapisovací spojení a čtení bez transakce. Ověří se až v implementaci.

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| CEO Review | `/plan-ceo-review` | Scope & strategy | 0 | — | — |
| Outside Review | codex via `/plan-eng-review` (native fallback) | Independent 2nd opinion | 1 | UNAVAILABLE | codex CLI not installed; native Plan fallback exceeded the 5-minute limit (2026-10-22, commit 95b5d1a) |
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 1 | ISSUES OPEN | 64 issues (6 architecture, 5 code quality, 5 performance, 48 test gaps on new code), 0 critical gaps; 13 mapped tasks T1 to T13 |
| Design Review | `/plan-design-review` | UI/UX gaps | 0 | — | — |
| DX Review | `/plan-devex-review` | Developer experience gaps | 0 | — | — |

- **OUTSIDE COVERAGE:** provider codex, phase plan-review, unavailable (CLI not installed; native fallback produced no complete report within five minutes), no findings; coverage missing, not clean.
- **VERDICT:** no review CLEAR; Eng Review ISSUES OPEN with all 64 issues mapped to tasks T1 to T13 and 0 unresolved decisions (D1 = A, D2 = A); scope accepted as-is; eng review required.

NO UNRESOLVED DECISIONS
