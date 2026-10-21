# Test R13: harness pro Scaleway

Tento adresář obsahuje vše, co je potřeba k provedení testu R13 z návrhu
[nylzo-architecture-research.md](../../docs/designs/nylzo-architecture-research.md)
(sekce „Nasazení veřejné instance (Scaleway)“, riziko R13, rozhodnutí OD21,
Next Steps 1 a The Assignment). Test rozhoduje premisu P3: zda veřejná instance
Nylzo může běžet na Scaleway Serverless Containers, Serverless SQL a Object Storage
do 100 Kč měsíčně. Nasazení dělá vlastník účtu; harness nic nenasazuje sám.

## Co test rozhoduje

Premisa P3 padá, když nastane kterákoli z těchto tří věcí:

1. 5 MB POST neprojde přes ingress Serverless Containers do kontejneru.
2. Studený start kontejneru po 15 minutách nečinnosti trvá déle než 3 s.
3. Běh úklidové úlohy a probouzení Serverless SQL každých 15 minut prodraží provoz
   nad 100 Kč měsíčně.

Vedle toho test měří, co návrh potřebuje k implementaci: název hlavičky
s IP klienta za ingressem (`NYLZO_PROXY_MODE=scaleway`), zda ingress tělo bufferuje
(AK22), zda TLS edge vyjedná X25519MLKEM768 (AK26), cenu útoku „drž vše vzhůru“
(jeden požadavek za minutu po dobu 24 h), latenci probuzení databáze (KD9) a zda jdou
vypnout zálohy Serverless SQL (OD20). Když některá podmínka neprojde, veřejná instance
jde na záložní hosting v pořadí OD21 a adaptér Postgres + S3 zůstane volitelný.

## Obsah adresáře

| Cesta | Co to je |
| --- | --- |
| `echo/` | Go 1.27 server jen ze stdlib: `POST /echo` (tělo do 6 MiB, vrátí počet bajtů, čas čtení a hlavičky), `GET /healthz`, `GET /start` (čas startu procesu). Naslouchá na `$PORT`, jinak 8080. Timeouty podle návrhu: ReadHeaderTimeout 5 s, ReadTimeout 120 s, WriteTimeout 120 s, IdleTimeout 30 s, MaxHeaderBytes 8 KiB. Umí HTTP/1.1 i h2c. |
| `dbwake/` | Go program pro Serverless Job: připojí se k Postgresu z `DATABASE_URL` (pgx v5.11.0), změří připojení a `SELECT 1` dvakrát, přečte `pg_postmaster_start_time()` a vypíše jeden řádek JSON. Končí s kódem 1 při chybě. `DBWAKE_TIMEOUT` (výchozí `90s`) omezuje celý běh. |
| `measure/post-5mb.sh` | (a) 5 MiB POST na `/echo` s kontrolou bajtů, lživý Content-Length, chunked tělo, sondy limitu 6 MiB + 1 B a 16 MiB. |
| `measure/cold-start.sh` | (b) zahřeje instanci, počká 16 minut, změří první odpověď `/start` a hned teplou odpověď pro srovnání. |
| `measure/keep-alive-24h.sh` | (c) jeden `GET /start` za minutu po dobu 24 h, CSV s časy a detekcí restartů instance. |
| `measure/tls-pq.sh` | (d) `openssl s_client -groups X25519MLKEM768` proti doméně instance. |
| `measure/headers.sh` | (e) čistý a podvržený POST na `/echo`, výpis hlaviček a verdikt pro `NYLZO_PROXY_MODE=scaleway` a `NYLZO_TRUSTED_PROXIES`. |
| `measure/local-smoke.sh` | sestaví echo image, spustí ho v Dockeru a prožene skripty (a), (e) a (b) s nulovou nečinností. |
| `measure/lib.sh` | sdílené funkce skriptů. |
| `results/` | sem skripty ukládají CSV a textové výstupy (gitignore, výsledky se do repozitáře dostanou přes vyplněnou šablonu níže). |

Každý skript má `set -euo pipefail` a nápovědu přes `--help`. Skripty jsou psané pro
bash 3.2 z macOS a BSD `sed`; potřebují jen `curl`, `sed`, `awk`, `head` a `mktemp`.
JSON parsují jednoduchým `sed`, protože echo vrací kompaktní JSON s pevnými klíči.

## Předpoklady na notebooku

- Go 1.27 (`mise install go@1.27.3`), Docker Desktop s buildx, `curl`.
- OpenSSL 3.5 nebo novější pro test (d): `brew install openssl@3`; LibreSSL v macOS
  X25519MLKEM768 neumí. Skript si Homebrew OpenSSL najde sám.
- Scaleway CLI: `brew install scw`, pak `scw init` s API klíčem z konzole
  (IAM > API keys). Výchozí region nastav na `fr-par`.
- Účet Scaleway s platební kartou. Podle dokumentace jde účet založit bez karty,
  ale objednání zdrojů kartu vyžaduje; Scaleway strhne a vrátí autorizaci 1 EUR
  a předplacené virtuální karty odmítá.

## Lokální ověření před nasazením

```bash
cd tools/r13/echo && go build ./... && go vet ./... && go test ./...
```

```bash
cd tools/r13/dbwake && go build ./... && go vet ./...
```

```bash
docker build --platform linux/amd64 -t r13-echo:local tools/r13/echo
```

```bash
docker build --platform linux/amd64 -t r13-dbwake:local tools/r13/dbwake
```

```bash
tools/r13/measure/local-smoke.sh
```

Kouřový test ověřuje jen funkčnost harnessu a trvá zhruba 2,5 minuty, protože případ
s Content-Length větším než tělo čeká na ReadTimeout serveru (120 s). Na Apple Silicon
běží amd64 image pod emulací, takže naměřené časy o Scaleway nic neříkají. Pokud `docker build`
selže na `gcr.io` kvůli přihlašovacímu helperu `gcloud` v `~/.docker/config.json`,
spusť build s `DOCKER_CONFIG` ukazujícím na kopii konfigurace bez `credHelpers`
(image distroless je veřejný a přihlášení nepotřebuje).

Program `dbwake` jde vyzkoušet proti místnímu Postgresu:

```bash
docker run -d --rm --name r13-pg -e POSTGRES_PASSWORD=r13pass -p 127.0.0.1:15432:5432 postgres:18-alpine
```

```bash
cd tools/r13/dbwake && DATABASE_URL='postgres://postgres:r13pass@127.0.0.1:15432/postgres?sslmode=disable' go run .
```

## Nastavení na Scaleway

Všechno v regionu `fr-par`, protože Serverless SQL existuje jen v Paříži. Názvy
voleb jsou z dokumentace Scaleway a CLI k 21. 10. 2026; konzole je může přejmenovat,
v CLI platí `scw <produkt> <příkaz> --help`.

### 1. Projekt a API klíč

1. V konzoli vytvoř projekt `nylzo-r13` (Organization > Projects), ať jde po testu
   smazat jedním krokem.
2. IAM > API keys: vytvoř klíč pro sebe s výchozím projektem `nylzo-r13` a zadej ho do
   `scw init`. Ulož secret key do správce hesel; konzole ho ukáže jen jednou.
3. Zapiš si ID projektu (`scw account project list`).

### 2. Container Registry a push image

Serverless Containers umí i veřejné externí registry (dokumentace uvádí Docker Hub
jako příklad a soukromé externí registry výslovně nepodporuje), ale kvůli rate
limitům je nedoporučuje. Pro test použij Scaleway Container Registry; zároveň si zapiš
do šablony, zda konzole nabídla „External“ registry a přijala veřejnou adresu
z ghcr.io (rozhoduje OD17).

```bash
scw registry namespace create name=nylzo-r13 region=fr-par
```

```bash
scw registry login region=fr-par
```

Ruční varianta přihlášení podle dokumentace „Build and push a container image“:
`docker login rg.fr-par.scw.cloud/nylzo-r13 -u nologin --password-stdin` se secret
key na vstupu. Pak sestav a nahraj oba image, vždy pro `linux/amd64`:

```bash
docker build --platform linux/amd64 -t rg.fr-par.scw.cloud/nylzo-r13/r13-echo:1 tools/r13/echo && docker push rg.fr-par.scw.cloud/nylzo-r13/r13-echo:1
```

```bash
docker build --platform linux/amd64 -t rg.fr-par.scw.cloud/nylzo-r13/r13-dbwake:1 tools/r13/dbwake && docker push rg.fr-par.scw.cloud/nylzo-r13/r13-dbwake:1
```

### 3. Serverless Container s echem

Dokumentované meze: 70 až 6000 mvCPU, 128 až 12228 MB paměti, timeout požadavku
10 s až 60 min, souběžnost 80 požadavků na instanci, usnutí po 15 minutách bez
provozu, HTTP/1.1 nebo HTTP/2 (h2c), image `linux/amd64`. Limit velikosti těla
dokumentace neuvádí, proto se měří.

V konzoli: Serverless > Containers > Create namespace (`nylzo-r13`, fr-par) >
Deploy container > registry Scaleway, image `r13-echo:1`, port 8080, nejmenší vCPU
a 128 MB, autoscaling min 0 a max 1, timeout 120 s, protokol HTTP/1 (h2c až
v druhém kole, echo ho umí), sandbox v2, privacy public, health check
`GET /healthz`. Přes CLI:

```bash
scw container namespace create name=nylzo-r13 region=fr-par
```

```bash
scw container container create namespace-id=<NAMESPACE_ID> name=r13-echo image=rg.fr-par.scw.cloud/nylzo-r13/r13-echo:1 port=8080 protocol=http1 min-scale=0 max-scale=1 mvcpu-limit=70 memory-limit-bytes=134217728 timeout=120s privacy=public sandbox=v2 liveness-probe.http.path=/healthz region=fr-par
```

Pokud kontejner po `create` zůstane ve stavu `created`, nasaď ho tlačítkem Deploy
v konzoli (CLI má podle dokumentace jen `scw container deploy` pro build ze zdrojů;
zkontroluj `scw container container --help`, zda tvoje verze nemá i `deploy`).
Adresu instance (`https://<id>.functions.fnc.fr-par.scw.cloud`) vrátí
`scw container container get <CONTAINER_ID>` jako `domain_name`; zapiš ji jako
`BASE` pro skripty. Max 1 instance je záměr z návrhu (strop nákladů při útoku).

Ověření: `curl -s $BASE/healthz` vrátí `ok`, `curl -s $BASE/start` vrátí JSON
se `started_at`.

### 4. Serverless SQL

Dokumentované vlastnosti: minimum 0 vCPU, maximum 15 vCPU, 4 GB RAM na vCPU; po
5 minutách bez dotazu přechází databáze do stavu idle a za idle se výpočet neúčtuje,
úložiště ano; v aktivním stavu je spodní hranice 1 vCPU a 4 GB RAM; dotazy během
probouzení „mohou mít několik sekund zpoždění“; automatické zálohy denně
s uchováním 7 dní v ceně. Cena výpočtu 0,13752 EUR za vCPU-hodinu bez DPH
(příloha B rešerše).

V konzoli: Databases > Serverless SQL > Create database, region fr-par, název
`nylzo-r13`, vCPU min 0, vCPU max 1 (nejnižší hodnota, kterou konzole dovolí; pokud
dovolí jen vyšší, zapiš ji do šablony). Přes CLI:

```bash
scw sdb-sql database create name=nylzo-r13 cpu-min=0 cpu-max=1 region=fr-par
```

Připojení: IAM > Applications > vytvoř aplikaci `r13-dbwake` s politikou pro
datový přístup k Serverless SQL (název sady oprávnění vyber v nabídce IAM, kam
patří `ServerlessSQLDatabase…ReadWrite`) a vytvoř jí API klíč. Connection string
má podle dokumentace tvar

```
postgres://<ID_IAM_APLIKACE>:<SECRET_KEY>@<HOST_DATABAZE>:5432/<NAZEV_DB>?sslmode=require
```

kde uživatel je ID IAM uživatele nebo aplikace, heslo je secret key a `sslmode=require`
je povinný. Host vrátí `scw sdb-sql database get <DATABASE_ID>` nebo záložka Overview
v konzoli. První připojení vyzkoušej z notebooku:

```bash
cd tools/r13/dbwake && DATABASE_URL='postgres://…?sslmode=require' go run .
```

Zapiš `connect_ms`, `select1_ms` a `db_uptime_s`. Po 10 minutách klidu spusť znovu
a zapiš totéž jako latenci probuzení po idle (KD9).

### 5. Serverless Job s dbwake

Dokumentované meze: nejvýše 6 vCPU a 16 GB, timeout až 24 h, image `linux/amd64`,
cron s pěti poli (`*/15 * * * *` = každých 15 minut), časové pásmo jde v záložce
Triggers změnit. Nejmenší vCPU a paměť dokumentace neuvádí; vyber nejmenší hodnoty,
které konzole nabídne, a zapiš je.

V konzoli: Serverless > Jobs > Create job > registry Scaleway, image `r13-dbwake:1`,
region fr-par, nejmenší vCPU a paměť, timeout 2 min, proměnná prostředí
`DATABASE_URL` (pro test stačí obyčejná proměnná, databáze i klíč jsou na jedno
použití; pro ostrý provoz patří do Secret Manageru), cron `*/15 * * * *`. Přes CLI:

```bash
scw jobs definition create name=r13-dbwake image-uri=rg.fr-par.scw.cloud/nylzo-r13/r13-dbwake:1 cpu-limit=<NEJMENSI_mvCPU> memory-limit=<NEJMENSI_MiB> local-storage-capacity=<NEJMENSI_MiB> job-timeout=120 environment-variables.DATABASE_URL='postgres://…?sslmode=require' cron-schedule.schedule='*/15 * * * *' cron-schedule.timezone=UTC region=fr-par
```

První běh spusť ručně a ověř v logu běhu řádek JSON z dbwake:

```bash
scw jobs definition start <JOB_DEFINITION_ID> region=fr-par
```

```bash
scw jobs run list job-definition-id=<JOB_DEFINITION_ID> region=fr-par
```

Logy běhu jsou v konzoli u daného běhu (Cockpit). Úloha nikdy nevolá webový
kontejner, přesně jako v návrhu.

### 6. Alarm útraty 3 EUR

Konzole: Billing (menu vpravo nahoře) > Consumption > Billing alerts > Create
a billing alert. Rozpočet je měsíční částka v EUR, práh se zadává v procentech
rozpočtu, kanál SMS, e-mail nebo webhook, nejvýše 10 alarmů. Nastav rozpočet 3 EUR
a dva alarmy, 50 % a 100 %. Dokumentace upozorňuje, že alarm počítá s částkou po
slevách a DPH a je jen odhad; za platný se počítá až měsíční účet. Při cronu každých
15 minut může alarm vystřelit během testu (viz odhad ceny níže); to je výsledek, ne
chyba.

### 7. Zálohy Serverless SQL: jak zjistit, zda jdou vypnout

Dokumentace: databáze se zálohuje automaticky každý den ve stejný čas, zálohy se
uchovávají 7 dní, ruční zálohy Scaleway nepodporuje. Vypnutí ani smazání zálohy
dokumentace nezmiňuje. Postup ověření:

1. Konzole > Databases > Serverless SQL > databáze > záložka Backups: hledej
   přepínač automatických záloh nebo nastavení retence; u existující zálohy
   rozbal nabídku a zapiš, jaké akce nabízí (dokumentované jsou Restore, Create
   database from backup a Prepare export).
2. CLI: `scw sdb-sql backup --help` a `scw sdb-sql backup list database-id=<DATABASE_ID>`
   druhý den testu; dokumentované podpříkazy jsou `list`, `get` a `export`, `delete`
   dokumentace neuvádí.
3. API reference Serverless SQL (`developers.scaleway.com`): hledej endpoint pro
   smazání zálohy nebo pole pro vypnutí záloh v definici databáze.
4. Když nic z toho neexistuje, zapiš „nelze vypnout ani smazat, retence 7 dní“;
   stránka transparentnosti pak musí uvádět, že metadata mohou u poskytovatele
   přežít až 7 dní po smazání (OD20).

## Průběh měření

Skripty berou adresu instance jako první argument (`https://<id>.functions.fnc.fr-par.scw.cloud`
nebo vlastní doména). Po každém nasazení kontejneru startuje nová instance i při
min 0, takže studený start měř až po více než 15 minutách od nasazení. Health check
podle dokumentace instanci nebudí, ale cokoli jiného ano: během čekání na usnutí
nesmí na adresu sahat prohlížeč, uptime monitor ani jiný skript.

Den 0 (po nasazení, zhruba hodina):

```bash
tools/r13/measure/headers.sh "$BASE"
```

```bash
tools/r13/measure/post-5mb.sh "$BASE"
```

```bash
tools/r13/measure/tls-pq.sh "$BASE"
```

Spusť dbwake ručně (bod 5) a nech cron běžet. Pokud chceš změřit i h2c, přepni
protokol kontejneru na HTTP/2 a zopakuj `headers.sh` a `post-5mb.sh`; echo vrací
`proto`, takže je vidět, co do kontejneru dorazilo.

Den 0 večer až den 1 (3 × 17 minut, při nichž se instance nesmí dotknout nic jiného):

```bash
tools/r13/measure/cold-start.sh "$BASE" --repeat 3
```

Den 1 až den 2 (24 h, notebook s napájením a stabilní sítí; skript se obalí do
`caffeinate`):

```bash
tools/r13/measure/keep-alive-24h.sh "$BASE"
```

Před startem a po skončení si zapiš z Billing > Consumption spotřebu Serverless
Containers (GB-s, vCPU-s, EUR), Serverless Jobs a Serverless SQL (vCPU-hodiny, EUR).
Nech úlohu běžet nejméně 48 h, ať jsou v konzoli dva celé dny. Pokud rozpočet
dovolí, přepni potom cron na `0 * * * *` na dalších 24 h a změř druhou variantu
z návrhu (úklid po 60 minutách).

Den 3: odečti spotřebu, projdi zálohy (bod 7), vyplň šablonu, ukliď (níže).

## Jak odečíst cenu a co čekat podle dokumentace

Spotřeba je v konzoli Billing > Consumption po produktech. Dokumentace neuvádí, jak
často se údaje obnovují; počítej se zpožděním a odečítej až druhý den. Ceny Scaleway
jsou bez DPH; 100 Kč měsíčně odpovídá zhruba 4 EUR s DPH, tedy zhruba 3,3 EUR bez
DPH při kurzu kolem 24,5 Kč za euro (aktuální kurz: ČNB).

Co z dokumentace plyne předem, a co má měření potvrdit nebo vyvrátit:

- Serverless SQL přechází do idle až po 5 minutách bez dotazu a v aktivním stavu
  má spodní hranici 1 vCPU. Při cronu každých 15 minut to znamená zhruba 96 × 5 min
  = 8 vCPU-hodin denně, tedy zhruba 1,1 EUR denně a 33 EUR měsíčně bez DPH. Při
  cronu každou hodinu 24 × 5 min = 2 vCPU-hodiny denně, zhruba 0,28 EUR denně
  a 8,3 EUR měsíčně. Obě varianty jsou nad 100 Kč. Návrh počítal s 1 až 2
  vCPU-hodinami měsíčně za předpokladu, že databáze usne hned po dotazu; tenhle
  předpoklad dokumentace nepotvrzuje. Rozhodne naměřená spotřeba Serverless SQL
  za den; pokud se potvrdí 5minutové okno, podmínka 3 projde jen s úklidem
  nejvýše jednou za několik hodin (líný úklid při požadavku plus lifecycle pravidlo
  bucketu), nebo platí OD21.
- Kontejner s 70 mvCPU a 128 MB držený vzhůru 24 h spotřebuje 10 800 GB-s a 6 048
  vCPU-s; za měsíc 324 000 GB-s a 181 000 vCPU-s, tedy uvnitř free tieru
  400 000 GB-s a 200 000 vCPU-s. S 256 MB (hodnota z návrhu pro ostrý server) je
  to 648 000 GB-s měsíčně, nad free tier. Cena za 24 h bude proto v konzoli
  nejspíš 0 EUR; užitečné číslo je spotřeba v GB-s a vCPU-s, kterou extrapoluješ
  na měsíc.
- Zda Serverless Jobs sdílejí free tier s Containers, dokumentace neříká; spotřebu
  Jobs zapiš zvlášť.
- Hlavičky: podle reference „Containers request headers“ Scaleway přepisuje
  `X-Real-IP` i když ji klient poslal, `X-Forwarded-For` může klient upravit
  a `X-Envoy-External-Address` je zastaralá. Skript `headers.sh` to ověří
  podvrženým požadavkem.

## Úklid po testu

Smaž v tomto pořadí, ať nic neběží dál: definici úlohy, kontejner a jeho namespace,
databázi (tím i zálohy po 7 dnech, pokud je nejde smazat dřív), namespace registry,
IAM aplikaci a její klíč, nakonec projekt `nylzo-r13`. Alarm útraty nech, pokud
účet zůstává.

```bash
scw jobs definition delete <JOB_DEFINITION_ID> region=fr-par
```

```bash
scw container container delete <CONTAINER_ID> region=fr-par
```

```bash
scw sdb-sql database delete <DATABASE_ID> region=fr-par
```

## Šablona výsledků

Zkopíruj do `tools/r13/results/vysledky.md`, vyplň a přilož CSV ze složky `results/`.

```markdown
# Výsledky testu R13

Datum měření: …  Účet/projekt: nylzo-r13 (fr-par)  Kurz ČNB EUR/CZK: …
Image: r13-echo:1 (digest …), r13-dbwake:1 (digest …)
Kontejner: … mvCPU, … MB, min 0 / max 1, timeout 120 s, protokol http1, sandbox v2
Serverless SQL: vCPU min 0 / max …  Job: … mvCPU, … MiB, cron */15 * * * *

## Výstupní podmínky R13

| # | Podmínka | Měření (skript, soubor) | Naměřeno | Projde? |
| --- | --- | --- | --- | --- |
| 1 | 5 MB POST projde | post-5mb.sh, results/post-5mb-….csv | received_bytes = … / 5 242 880, http …, time_total … s (3 běhy: …, …, …) | ano / ne |
| 2 | Studený start < 3 s | cold-start.sh --repeat 3, results/cold-start-….csv | time_total …, …, … s; first_byte …; uptime_ms při odpovědi … | ano / ne |
| 3 | Úklid + probouzení DB < 100 Kč/měsíc | Billing > Consumption, 2 celé dny | SQL … vCPU-h/den = … EUR/den; Jobs … GB-s/den = … EUR; × 30 = … EUR/měsíc bez DPH = … Kč s DPH | ano / ne |

Varianta cron 0 * * * * (pokud měřena): SQL … vCPU-h/den = … EUR/měsíc = … Kč.

## Doplňková měření

| Co | Naměřeno | Důsledek pro návrh |
| --- | --- | --- |
| Limit těla ingressu | 6 MiB + 1 B: http … (413 z echa = dorazilo do kontejneru); 16 MiB: http …, odpověď od … | limit NYLZO_MAX_FILE_BYTES … |
| Bufferování těla ingressem | chunked bez Content-Length: echo vidí content_length …, transfer_encoding …; CL větší než tělo: http … po … s; CL menší než tělo: http …, received … | AK22: kontrola Content-Length před čtením … |
| Hlavička s IP klienta | čistý: remote_addr …, X-Real-IP …, XFF …; podvržený: X-Real-IP …, XFF …, X-Envoy … | NYLZO_PROXY_MODE=scaleway čte …; NYLZO_TRUSTED_PROXIES … |
| Protokol do kontejneru | proto …, tls false/true, X-Forwarded-Proto … | h2c: ano / ne / neměřeno |
| X25519MLKEM768 na edge | vyjednaná skupina: … (default: …, kontrola X25519: …) | AK26 … |
| Latence probuzení DB | první dbwake po 10 min klidu: connect … ms, select1 … ms, db_uptime_s …; za běhu cronu: … | KD9 … |
| Usnutí DB | podle Cockpit/Consumption: vCPU klesá na 0 po … min od dotazu | interval úklidu … |
| Zálohy Serverless SQL | vypnout: lze / nelze; smazat: lze / nelze; retence … dní; zdroj: konzole / CLI / API | OD20: stránka transparentnosti uvádí … |
| Registry | Scaleway Registry: OK; externí veřejná (ghcr.io): nabídnuto ano / ne, funguje ano / ne | OD17 … |
| Vlastní doména a TLS | konzole nabízí custom domain: ano / ne; certifikát Let's Encrypt: … | otevřený bod návrhu … |
| „Drž vše vzhůru“ 24 h | keep-alive-….csv: požadavků …, chyb …, restartů …, p50 … s; Consumption: … GB-s, … vCPU-s, … EUR | strop při útoku … EUR/měsíc |

## Rozhodnutí

- Podmínky 1, 2 a 3 prošly: P3 platí, veřejná instance jde na Scaleway, adaptér
  Postgres + S3 je součástí první verze.
- Některá podmínka neprošla (která: …, proč: …): P3 padá, postup podle OD21:
  1. Scaleway STARDUST1-S s diskem a IPv4 (EU firma, zhruba 11 Kč plus extra,
     dostupnost nejistá);
  2. OVH VPS-1 (EU firma, zhruba 112 Kč s DPH při roční platbě, výjimka
     z rozpočtu zhruba 12 Kč);
  3. Fly.io (americká firma, zhruba 51 Kč) jen s výslovnou výjimkou z požadavku
     na EU firmu.
  Adaptér Postgres + S3 zůstává volitelný pro pozdější verzi.
- Mezikrok jen pro podmínku 3: pokud 1 a 2 prošly a cena padá jen na probouzení
  databáze, zapiš, při jakém intervalu úklidu (…) se provoz vejde do 100 Kč,
  a zda je takový interval slučitelný se zárukou úklidu v návrhu.
```

## Zdroje (otevřeno 21. 10. 2026)

- https://www.scaleway.com/en/docs/serverless-containers/reference-content/containers-limitations/
- https://www.scaleway.com/en/docs/serverless-containers/reference-content/request-headers/
- https://www.scaleway.com/en/docs/serverless-containers/faq/
- https://www.scaleway.com/en/docs/serverless-containers/how-to/deploy-container/
- https://www.scaleway.com/en/docs/serverless-containers/how-to/build-push-container-image/
- https://www.scaleway.com/en/docs/serverless-sql-databases/reference-content/serverless-sql-databases-overview/
- https://www.scaleway.com/en/docs/serverless-sql-databases/how-to/connect-to-a-database/
- https://www.scaleway.com/en/docs/serverless-sql-databases/how-to/manage-backups/
- https://www.scaleway.com/en/docs/serverless-sql-databases/faq/
- https://www.scaleway.com/en/docs/serverless-jobs/reference-content/jobs-limitations/
- https://www.scaleway.com/en/docs/serverless-jobs/reference-content/cron-schedules/
- https://www.scaleway.com/en/docs/billing/how-to/use-billing-alerts/
- https://www.scaleway.com/en/developers/cli/container/, …/cli/sdb-sql/, …/cli/jobs/, …/cli/registry/
- Ceny a free tier: https://www.scaleway.com/en/pricing/serverless/ a
  https://www.scaleway.com/en/pricing/managed-databases/ (přes přílohu B rešerše,
  `docs/research/B-hosting.md`)
