# Rešerše I: hosting prototypu Nylzo

Ověřeno 23. 10. 2026 na stránkách poskytovatelů (seznam zdrojů na konci). Kurz ČNB 23. 10. 2026: 24,335 Kč za EUR a 21,741 Kč za USD (https://www.cnb.cz/en/financial-markets/foreign-exchange-market/central-bank-exchange-rate-fixing/central-bank-exchange-rate-fixing/daily.txt). Ceny jsou bez DPH, pokud není uvedeno jinak; částky s 21 % DPH jsou můj výpočet.

Stav údajů: ověřeno znamená citaci z otevřené stránky poskytovatele. Částečně znamená, že stránka tvrzení podporuje jen zčásti, nebo že jde o vyjádření zaměstnance na fóru či blogu. Neověřeno znamená, že oficiální zdroj jsem nenašel.

Požadavky vycházejí z D57: jeden kontejner `nylzo serve` se SQLite a bloby na trvalém POSIX svazku, jediná instance, dlouho běžící proces (úklid každých 5 minut), těla do 5 410 880 B nahrávaná až asi 6 minut, TLS smí ukončit hosting, skutečná IP klienta v hlavičce, kterou klient nepodvrhne, a krátké nebo žádné access logy.

## Shrnutí a doporučení

Doporučení: malý VPS s Compose a Caddy, které repozitář už má v `deploy/compose/`. Konkrétně DigitalOcean Droplet Basic 1 GiB ve Frankfurtu (FRA1) za 6 USD měsíčně, tedy 130 Kč bez DPH a asi 158 Kč s DPH. Server nepotřebuje žádnou změnu, `NYLZO_TRUSTED_PROXIES` je v `compose.yaml` nastavené na pevné adresy Caddy, Caddy nepíše access log a TLS končí na vlastním stroji, takže hlavičky s klíči nevidí žádná cizí proxy. Limity těla a timeouty určuje jen `nylzo`. Cenou za to je správa stroje: aktualizace systému, firewall a SSH.

Hetzner CX23 (2 vCPU, 4 GB, 40 GB, 5,99 EUR včetně IPv4, s DPH asi 176 Kč) je lepší poměr výkonu a ceny od EU firmy, ale Hetzner od 26. 6. 2026 omezuje vytváření nových cloudových serverů a 23. 10. 2026 ukazoval všechny sdílené plány jako nedostupné. Pokud vlastníkovi konzole Hetzner server vytvořit dovolí, je to rovnocenná volba se stejným postupem.

Druhá volba: Fly.io, pokud vlastník nechce spravovat server. Jeden Machine s 256 MB a 5GB volume ve Frankfurtu vyjde odhadem na 3,3 USD měsíčně (asi 71 Kč bez daně). Fly ale nemá free tier, server by potřeboval nový režim proxy pro hlavičku `Fly-Client-IP` a dvě věci Fly nedokumentuje: zda proxy přepíše `Fly-Client-IP` poslanou klientem a jaké access logy proxy ukládá. Před ostrým provozem je nutný test.

Vercel nevyhovuje ve čtyřech bodech: tělo požadavku má limit 4,5 MB (zpráva má až 5,41 MB), funkce na Hobby běží nejvýš 300 s, platforma nemá trvalý lokální disk a kontejner po 5 minutách bez provozu usne. Hobby je navíc jen pro nekomerční osobní použití. Nové Vercel Daemons běží dlouho, ale provoz nepřijímají a trvalé úložiště nemají.

Z prověřených platforem dává trvalý svazek zdarma a bez karty jen Northflank Sandbox (do 20 USD měsíčně zdrojů); Railway Free má volume 0,5 GB, ale vyřazuje ho limit nahrávání. Timeouty, limit těla a chování `X-Forwarded-For` tam ale nejsou zdokumentované, takže bez testu ho doporučit nejde.

Vyřazené: Railway (tělo se musí nahrát do 5 minut, pomalý klient potřebuje 5,6 minuty), Koyeb (timeout 100 s, volumes jen v preview), Render (funguje, ale 7,25 USD a nezdokumentovaný limit těla i chování `X-Forwarded-For`), Google Cloud Run, Scaleway Serverless Containers a DigitalOcean App Platform (žádný trvalý POSIX svazek).

## Srovnání

| Platforma | Trvalý svazek | Max. tělo | Timeout požadavku | IP klienta, adresy proxy | Access logy platformy | TLS, vlastní doména | Region | 1 malá instance + 1 GB | Free tier |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| VPS + Compose: DigitalOcean Basic | lokální disk VM, 25 GiB (plán za 6 USD) | bez limitu platformy | bez limitu platformy | Caddy nastaví `X-Forwarded-For` sám, `NYLZO_TRUSTED_PROXIES` = pevné adresy Caddy | žádné; Caddy access log vypnutý | Caddy, automatické certifikáty, HTTP/2 a HTTP/3 | FRA1, AMS3 a další; US firma | 6 USD (1 GiB), 4 USD (512 MiB) | ne |
| VPS + Compose: Hetzner CX23 | lokální disk, 40 GB NVMe | jako výše | jako výše | jako výše | jako výše | jako výše | NBG1, HEL1; DE firma | 5,99 EUR včetně IPv4 | ne; nové servery omezeně dostupné |
| VPS + Compose: OVHcloud VPS-1 | lokální disk, 40 GB NVMe | jako výše | jako výše | jako výše | jako výše | jako výše | EU datacentra; FR firma | od 3,81 EUR, roční platba předem | ne |
| Fly.io | volume na NVMe hostitele, 1 až 500 GB | nezdokumentováno; zaměstnanec 2023: těla se streamují, limit není | max. délka nezdokumentována; 60 s bez dat zavře spojení (zaměstnanec 2022) | `Fly-Client-IP`, přepis nezdokumentován; `X-Forwarded-For` lze podvrhnout; adresy proxy nezdokumentovány | stdout aplikace 7 dní; logy proxy nezdokumentovány | ano, prvních 10 certifikátů zdarma, HTTP/2 | fra, ams, cdg, arn, lhr; US firma | 2,34 USD (iad), ve fra odhadem 2,68 USD | ne; zkušební 2 h běhu nebo 7 dní |
| Northflank | volume, služba pak jen 1 instance | nezdokumentováno | nezdokumentováno, nastavitelné po cestách | `X-Forwarded-For` od load balanceru, append nebo přepis nezdokumentován | nezdokumentováno | ano, HTTP/2 | neověřeno | odhadem 2,9 USD, v Sandboxu 0 | Sandbox do 20 USD/měs. zdrojů, bez karty |
| Railway | volume, 1 na službu, bez replik | nezdokumentováno | 15 min při přenosu dat; tělo musí být nahrané do 5 min | `X-Real-IP`, přepis nezdokumentován | HTTP logy s cestou a IP: Free 3 dny, Hobby 7, Pro 30 | ano, HTTP/2 | neověřeno | Hobby 5 USD včetně 5 USD spotřeby | trial 5 USD na 30 dní, pak 1 USD/měs., volume 0,5 GB |
| Render | persistent disk jen u placené služby | nezdokumentováno | 100 min | `X-Forwarded-For`, první IP je klient (zaměstnanec 2021) | HTTP request logy až od Pro workspace | ano, HTTP/2 | Frankfurt a další | 7,25 USD (Starter + 1 GB) | Free služba bez disku |
| Koyeb | volume jen v preview, „only suitable for testing“ | nezdokumentováno | 100 s | `X-Forwarded-For` s připojenou IP | neověřováno | neověřováno | volumes jen fra, was | Pro 29 USD + výpočet | free instance bez volume |
| Vercel | žádný trvalý disk | 4,5 MB | Hobby 300 s, Pro 800 s | `x-forwarded-for` Vercel přepisuje | runtime logy 1 h (Hobby) | neověřováno | výchozí iad1 | nevyhovuje | Hobby jen nekomerčně |
| Google Cloud Run | jen Cloud Storage FUSE (ne POSIX), NFS, paměť | 32 MiB s HTTP/1, bez limitu s HTTP/2 | 60 min | neověřováno | neověřováno | neověřováno | neověřováno | nevyhovuje | neověřováno |
| Scaleway Serverless Containers | jen dočasný disk | nezdokumentováno | 10 s až 60 min | `X-Real-IP` předpoklad, R13 neproběhl | neověřováno | neověřováno | fr-par, nl-ams, pl-waw | nevyhovuje | ano, ale bez svazku |
| DigitalOcean App Platform | „does not support volumes“ | nezdokumentováno | nahrávání souborů 600 s | neověřováno | neověřováno | neověřováno | neověřováno | nevyhovuje | neověřováno |

## Kandidáti

### VPS s Compose a Caddy

Platforma nepřidává žádnou proxy ani limit. Celé chování popisuje `deploy/compose/README.md`: Caddy s pevnými adresami 172.30.80.2 a fd1f:9791:e50b::2, `NYLZO_TRUSTED_PROXIES` jen na ně, Caddy zahodí `X-Forwarded-For` od klienta a nastaví vlastní, access log a chybové logy s detaily požadavku má vypnuté, nečinné spojení na `nylzo` drží nejvýš 25 s (server zavírá po 30 s). Kontejner `nylzo` má read-only kořen, žádné capabilities a pepper jako Docker secret v souboru (`NYLZO_PEPPER_FILE`).

DigitalOcean Droplets (ověřeno, https://www.digitalocean.com/pricing/droplets):
- Basic Regular SSD: 512 MiB, 1 vCPU, 10 GiB, 500 GiB přenosu za 4 USD; 1 GiB, 1 vCPU, 25 GiB, 1 000 GiB přenosu za 6 USD.
- „For Bundled Plans, a public IPv4 is included at no extra cost.“
- Zálohy jsou placený doplněk (20 % ceny týdně, 30 % denně). Pro OD20 je nezapínat.
- Další odchozí přenos 0,01 USD za GiB, hodinové účtování má strop 672 hodin měsíčně (https://docs.digitalocean.com/products/droplets/details/pricing/, stránka „Last verified 25 Aug 2026“).
- Frankfurt i Amsterdam jsou na seznamu datacenter. DigitalOcean je americká firma.

Hetzner Cloud (ověřeno):
- CX23: 2 vCPU, 4 GB, 40 GB NVMe, 20 TB, 5,99 EUR měsíčně bez DPH, „Price incl. IPv4“; CAX11 (Arm) 6,49 EUR; nejlevnější Regular Performance CPX02 (1 vCPU, 1 GB, 20 GB) 6,49 EUR (https://www.hetzner.com/cloud/cost-optimized/, https://www.hetzner.com/cloud/regular-performance/).
- 23. 10. 2026 byly všechny plány na obou stránkách označené „This product is currently unavailable. Please check back later.“
- Status page má od 26. 6. 2026 otevřené oznámení „Limited availability of cloud instances“ (https://status.hetzner.com/). FAQ (změna 27. 4. 2026): Hetzner „reserves the right to temporarily restrict the provisioning of new cloud servers at specific locations“, omezení se týká „a limited group of customers“ a doporučuje zkusit to další den (https://docs.hetzner.com/cloud/general/faq/).
- Ceny zvýšil 15. 6. 2026, CX23 z 3,99 na 5,49 EUR bez IPv4 (částečně, blog Northflank, https://northflank.com/blog/hetzner-cloud-server-price-increases).

OVHcloud VPS-1 (částečně): „From €3.81 ex. VAT/month“, 2 vCores, 4 GB, 40 GB NVMe, neomezený přenos, IPv4 v ceně, „Daily backup of the previous 24 hours“ (https://www.ovhcloud.com/en-ie/vps/). Podle rešerše B je cena „from“ při platbě na 12 měsíců předem. Zda jde denní záloha vypnout, jsem neověřil; obsahovala by šifrované bloby, což jde proti OD20.

### Fly.io

Ceny (ověřeno, https://docs.fly.io/about/pricing):
- shared-cpu-1x 256 MB 2,19 USD, 512 MB 3,69 USD, 1 GB 6,70 USD měsíčně v iad a ewr. Ceny se liší podle regionu; stránka uvádí jen příklad 1 GB ve fra za 7,73 USD. Pro 256 MB ve fra z toho odhaduji 2,53 USD a pro 512 MB 4,26 USD (můj výpočet, ne publikovaná cena).
- Volume 0,15 USD za GB a měsíc. Snapshoty 0,08 USD za GB, prvních 10 GB zdarma, denní snapshoty s retencí 5 dní jsou ve výchozím stavu zapnuté.
- Sdílená IPv4 a IPv6 zdarma, dedikovaná IPv4 2 USD. Prvních 10 certifikátů pro jeden hostname zdarma. Odchozí přenos v Evropě 0,02 USD za GB.
- „New organizations don't have a free tier or a monthly free usage allowance.“ Zkušební období: „up to 2 hours of Machine runtime or 7 days, whichever comes first“, bez karty; potom je karta nutná.

Volumes (ověřeno, https://docs.fly.io/volumes/overview a https://docs.fly.io/volumes/snapshots):
- „A Fly Volume is a slice of an NVMe drive on the same physical server as the Machine“, „It is not network storage.“ Lokální disk tedy hard linky i fsync adresáře zvládne; typ souborového systému stránka neuvádí.
- Jeden volume na jeden Machine, 1 až 500 GB, šifrování v klidu zapnuté.
- „If you only have a single copy of your data on a single volume, and that drive fails, then the data is lost.“ Fly doporučuje dva volumes, což Nylzo s jedním zapisovatelem SQLite nemůže.
- Snapshoty jde vypnout: `scheduled_snapshots = false` v `[mounts]`.

Síť a proxy:
- `Fly-Client-IP` je „The IP address of the client from the perspective of Fly Proxy“. U `X-Forwarded-For` dokumentace varuje „This header must be treated with caution to avoid spoofing attempts“ a uvádí, že nejpravější adresa je IP aplikace (https://docs.fly.io/networking/request-headers). Zda proxy přepíše `Fly-Client-IP` poslanou klientem, dokumentace neříká. Komunitní test z roku 2022 ukázal, že podvržené `X-Forwarded-For: 1.2.3.4` projde a skutečná adresa se připojí za něj (částečně, https://community.fly.io/t/recommended-setting-for-trust-proxy-on-express/6346).
- Ze kterých adres se proxy připojuje k Machine, Fly nedokumentuje. Komunitní příspěvek uvádí 172.16.0.0/16 s poznámkou, že si autor není jistý (neověřeno).
- Limit těla: zaměstnanec Fly v roce 2023: „That's not a limit.“ a „Request bodies are streaming, there's a little bit of buffering going on, but that's only for `fly-replay` header users.“ (částečně, https://community.fly.io/t/documentation-for-request-body-size-limit/15649).
- Timeout: zaměstnanec v roce 2022: „60 seconds without any data being sent or received will cause the connection to be closed.“ (částečně, https://community.fly.io/t/request-timeouts-on-fly-io/5653). Pomalý klient posílá data průběžně, takže by to vadit nemělo. Maximální délku požadavku dokumentace neuvádí.
- `[http_service]`: `auto_stop_machines` má výchozí hodnotu `"off"`, `min_machines_running` drží Machines jen v primárním regionu, `h2_backend` zapne h2c k aplikaci, `idle_timeout` nastaví nečinnost spojení k aplikaci, jednotku ale stránka neuvádí (https://docs.fly.io/reference/configuration).
- `fly deploy` má `--ha` s výchozí hodnotou true, které vytvoří záložní Machines (https://docs.fly.io/flyctl/deploy). Pro Nylzo je nutné `--ha=false`, jinak by vznikl druhý Machine s vlastním volume a vlastní databází.

Logy a soukromí: logy aplikace jsou stdout a vyhledávání v Grafaně je drží 7 dní (https://docs.fly.io/monitoring/logging-overview). O access lozích proxy (cesty, IP klientů) stránka ani zásady ochrany soukromí (účinné od 24. 9. 2026) nic neříkají (https://fly.io/legal/privacy-policy). Neověřeno, je to otázka na podporu.

Tajemství: `fly secrets set` vytváří proměnné prostředí. `[[files]]` se `secret_name` zapíše tajemství i do souboru, ale proměnná prostředí zůstane, takže soubor na Fly pepper z prostředí procesu neodstraní. Práva a vlastníka souboru dokumentace neuvádí.

Nasazení: `fly tokens create deploy --name <jméno> --expiry <doba>`, výchozí platnost tokenu je 20 let, dokumentace doporučuje „the shortest possible duration“ (https://docs.fly.io/security/tokens). Návod pro GitHub Actions používá secret `FLY_API_TOKEN` a `superfly/flyctl-actions/setup-flyctl` (https://docs.fly.io/launch/continuous-deployment-with-github-actions). OIDC z GitHub Actions do Fly jsem v dokumentaci nenašel. Hotový image jde nasadit přes `--image` nebo `[build] image`.

Vlastník volume: podle uživatele Kurt na fóru Fly z roku 2021 (roli stránka neuvádí) nastaví Fly vlastníka přípojného bodu na uživatele z image (částečně, https://community.fly.io/t/application-is-unable-to-create-directory-in-volume/915). Image Nylzo běží jako 65532, takže by to mělo fungovat; ověřit při prvním nasazení.

### Northflank

- Sandbox: „Free“, „No card needed“, „2 services, 2 jobs, and 1 database“, „Up to $20/mo of resources on Northflank Cloud“, „Always on, no sleeping“. Volumes jsou v tabulce plánů „Included“ i pro Sandbox (ověřeno, https://northflank.com/pricing).
- Ceny mimo Sandbox: CPU 0,01667 USD za vCPU-hodinu, paměť 0,00833 USD za GB-hodinu, disk 0,11657 USD za GiB a měsíc, odchozí přenos 0,06 USD za GB. 0,1 vCPU a 256 MB nepřetržitě plus 1 GiB disku vychází odhadem na 2,9 USD (můj výpočet), v Sandboxu tedy zdarma.
- Volume: „Services are limited to 1 instance.“, při restartu „the running container will always be terminated before the new one starts.“; vlastnictví svazku dostane skupina z image (https://northflank.com/docs/v1/application/databases-and-persistence/add-a-volume).
- IP klienta: „You can access the source IP of a request from the X-Forwarded-For header, which is attached to all HTTP/S requests by the Northflank load balancer.“ Zda hlavičku přepisuje, nebo připojuje, ani adresy load balanceru stránka neuvádí (https://northflank.com/docs/v1/application/network/networking-on-northflank).
- Výchozí timeout a limit těla nejsou zdokumentované; timeout jde nastavit po cestách a po vypršení vrací 408 (https://northflank.com/docs/v1/application/domains/use-path-based-routing). Pokud ingress stojí na Envoy, jeho výchozí timeout trasy je 15 s (neověřeno pro Northflank).
- Access logy ingressu a regiony Sandboxu: neověřeno.

### Railway

- Plány: Free 0 USD s 1 USD kreditu měsíčně, Hobby 5 USD včetně 5 USD spotřeby, Pro 20 USD. RAM 10 USD za GB a měsíc, CPU 20 USD za vCPU a měsíc, volume 0,15 USD za GB. Když kredit dojde, Railway „stop all of your workloads“. Volume: Free a Trial 0,5 GB, Hobby 5 GB (https://docs.railway.com/reference/pricing/plans, https://docs.railway.com/reference/pricing/free-trial).
- Vylučující limit: „HTTP requests can run for up to 15 minutes if data keeps transferring“ a „Request bodies must finish uploading within 5 minutes.“ (https://docs.railway.com/networking/public-networking/specs-and-limits). Zpráva 5 410 880 B při 128 kbit/s trvá 338 s, tedy 5,6 minuty.
- IP klienta: „`X-Real-IP` for identifying client's remote IP.“ Přepis nezdokumentován.
- Volume: „Docker images that run as a non-root UID by default will have permissions issues“, řešení `RAILWAY_RUN_UID=0`, tedy běh jako root (https://docs.railway.com/reference/volumes).
- HTTP logy obsahují cestu i IP klienta (`@srcIp`), retence Free 3 dny, Hobby 7, Pro 30; vypnutí nezdokumentováno (https://docs.railway.com/observability/logs).

### Render

- Starter 7 USD (512 MB, 0,5 CPU), persistent disk 0,25 USD za GB a měsíc, Hobby workspace 0 USD (ověřeno z HTML https://render.com/pricing). Disk jen u placené služby, „You can't scale a service to multiple instances if it has a disk attached“, s diskem nejsou zero-downtime deploye, denní snapshoty drží 7 dní (https://render.com/docs/disks).
- „Render web services allow HTTP responses to take up to 100 minutes.“ (https://render.com/docs/render-vs-vercel-comparison). Limit těla nezdokumentován.
- IP klienta: zaměstnanec Render v roce 2021 „we set the first IP in the list to the real client IP“ (částečně, https://feedback.render.com/features/p/send-the-correct-xforwardedfor). Server by potřeboval nový režim „první položka `X-Forwarded-For`“, který je bezpečný jen tehdy, když Render hodnotu od klienta opravdu přepíše; to dokumentace neříká.
- Logy: „If you have a Pro workspace or higher, Render generates a log entry for each HTTP request“; Hobby má logy aplikace 7 dní (https://render.com/docs/logging).

### Koyeb

- „The connection timeout for HTTP requests is set to 100 seconds.“ a „the last IP of the `x-forwarded-for` is the only IP we can certify as valid“ (https://www.koyeb.com/docs/reference/edge-network, aktualizováno 27. 5. 2026). 100 s na 6minutový přenos nestačí.
- „Volumes are in public preview and are currently only suitable for testing.“, jen regiony was a fra, 1 až 10 GB, ne na free a eco instancích (https://www.koyeb.com/docs/reference/volumes).
- Pro plán 29 USD měsíčně včetně 10 USD výpočtu (https://www.koyeb.com/docs/faqs/pricing).

### Vercel

- „The maximum payload size for the request body or the response body of a Vercel Function is 4.5 MB.“ Max. délka Hobby 300 s, Pro 800 s, beta 1 800 s (https://vercel.com/docs/functions/limitations, aktualizováno 24. 8. 2026).
- OCI image jde nasadit jako Vercel Function; „Functions that receive no traffic for five minutes in production or 30 seconds in preview scale down.“, limity jsou stejné jako u funkcí, trvalé úložiště stránka nezmiňuje (https://vercel.com/kb/guide/docker, aktualizováno 22. 10. 2026).
- Vercel Daemons: „Daemons don't receive traffic.“, „Persistent local storage“ je mezi nepodporovanými funkcemi betaverze (https://vercel.com/kb/guide/vercel-daemons, 15. 10. 2026).
- „the Hobby plan restricts users to non-commercial, personal use only“, runtime logy 1 hodina (https://vercel.com/docs/plans/hobby).
- Pro úplnost: Vercel `x-forwarded-for` přepisuje, „This restriction is in place to prevent IP spoofing.“ (https://vercel.com/docs/headers/request-headers).

### Google Cloud Run

32 MiB na požadavek u HTTP/1, „No limit if using HTTP/2 server.“, timeout až 60 minut (https://docs.cloud.google.com/run/quotas, aktualizováno 22. 10. 2026). Trvalý svazek je jen Cloud Storage FUSE („not a fully POSIX-compliant file system“, bez zamykání souborů) nebo NFS; obojí požadavek vylučuje (https://docs.cloud.google.com/run/docs/configuring/services/cloud-storage-volume-mounts).

### Scaleway Serverless Containers

Timeout 10 s až 60 min, „Temporary disk size“ do 24 000 MiB, uspání po 15 minutách; trvalé svazky stránka nezmiňuje (https://www.scaleway.com/en/docs/serverless-containers/reference-content/containers-limitations/, „Reviewed on September 17, 2025“). Pro architekturu se SQLite v procesu tedy nevyhovuje; test R13 zůstává jen pro případný přesun na Postgres a Object Storage.

### DigitalOcean App Platform

„App Platform does not support volumes.“ a „Data in the host instance's local filesystem is permanently lost after deployments“ (https://docs.digitalocean.com/products/app-platform/details/limits/).

### Další možnosti

- Google Compute Engine e2-micro v Always Free: jeden e2-micro v us-west1, us-central1 nebo us-east1, 30 GB-měsíců disku, 1 GB odchozího přenosu měsíčně (https://docs.cloud.google.com/free/docs/free-cloud-features, aktualizováno 20. 10. 2026). Odpovídá to asi 190 stažením zprávy maximální velikosti. Zda se platí za veřejnou IPv4, jsem neověřil. Jen USA.
- Oracle Cloud Always Free a Scaleway STARDUST1-S (asi 0,43 EUR měsíčně bez disku a IPv4, https://www.scaleway.com/en/pricing/virtual-instances/) popisuje rešerše B; obojí má nejistou dostupnost a Oracle může nečinnou instanci odebrat.

## Doporučené nasazení: DigitalOcean Droplet s Compose

Účet zakládá vlastník (D57). Potřebuje platební kartu a doménu.

1. Vytvořte Droplet: Basic, Regular SSD, 6 USD (1 GiB, 25 GiB), region FRA1 (nebo AMS3), aktuální Ubuntu LTS nebo Debian, zapnutá IPv6, přihlášení jen SSH klíčem, zálohy vypnuté (OD20).
2. Firewall (DigitalOcean Cloud Firewall nebo `ufw`): příchozí 22/tcp jen z vaší adresy, 80/tcp, 443/tcp a 443/udp.
3. Na serveru nainstalujte Docker Engine z repozitáře Dockeru (README: ne Docker Desktop, kvůli adresám klientů) a zapněte automatické bezpečnostní aktualizace. Aktualizace jádra potřebují restart, který znamená krátký výpadek.
4. DNS: záznamy A a AAAA domény na adresy Dropletu.
5. Zkopírujte `deploy/compose/` na server a postupujte podle `deploy/compose/README.md`:
   - ověřte digest a podpis image (krok 1) a do `.env` dejte `NYLZO_IMAGE` s `@sha256:` a `NYLZO_DOMAIN`;
   - `NYLZO_GLOBAL_DAILY_CREATES=350`: na disku 25 GiB se vejde 8 × 350 × 5,41 MB ≈ 15,2 GB zpráv, systém a image zaberou zbytek a `NYLZO_MIN_FREE_BYTES` (1 GiB) zůstane volný; u plánu za 4 USD (10 GiB) dejte zhruba 120;
   - pepper: `openssl rand -hex 32 > secrets/pepper`, vlastník 65532:65532, práva 0400; Compose ho předá jako Docker secret a `NYLZO_PEPPER_FILE=/run/secrets/nylzo_pepper`;
   - proxy: `NYLZO_PROXY_MODE` nenastavujte, `NYLZO_TRUSTED_PROXIES=172.30.80.2/32,fd1f:9791:e50b::2/128` už je v `compose.yaml`;
   - data: volume `nylzo-data` na `/home/nonroot`, `NYLZO_DATA_DIR=/home/nonroot`;
   - `docker compose up -d`.
6. Kontrola: `curl -fsS https://<doména>/healthz`. Compose nemá healthcheck, protože distroless image nemá shell ani curl, takže `/healthz` hlídejte externím monitorem.
7. Ověření požadavků: vytvořit a přečíst zprávu 5 MB; pomalé nahrání `curl --limit-rate 16000` (128 kbit/s, asi 338 s) musí projít; požadavek s podvrženým `X-Forwarded-For: 1.2.3.4` musí spadnout do kyblíku skutečné adresy.
8. Nasazení dalších verzí z GitHub Actions. Holý VPS OIDC z GitHubu neumí, možnosti jsou tři:
   - ručně podle README (ověřit nový image, změnit `NYLZO_IMAGE`, `docker compose up -d`); pro prototyp stačí;
   - workflow po vydání se připojí SSH klíčem uloženým v GitHub secrets; klíč má v `authorized_keys` volby `restrict,command="/usr/local/bin/nylzo-deploy"`, takže umí jen spustit skript, který přijme tag verze, ověří podpis image přes `cosign verify` jako v README a spustí Compose; klíč je dlouhodobý, ale nic jiného neudělá;
   - server si nová vydání stahuje sám (systemd timer, který ověří podpis a nasadí); GitHub pak k serveru nemá žádný přístup.

Po milníku 4 image spouští `nylzo` a Compose mu předává příkaz `serve` (D53, plán milníku 4); zkontrolujte, že `compose.yaml` na serveru odpovídá verzi image.

## Druhá volba: Fly.io

Předpoklad: v serveru je režim `NYLZO_PROXY_MODE=fly` (kapitola Změny serveru).

1. Vlastník založí účet a přidá kartu. Aplikaci dejte do organizace, kde neběží jiné aplikace: aplikace ve stejné organizaci se k Machine dostanou přes privátní síť přímo, mimo proxy, a mohly by `Fly-Client-IP` podvrhnout (server naslouchá na `:8080` na všech rozhraních).
2. `fly apps create <název>` a `fly volumes create nylzo_data --region fra --size 5 --scheduled-snapshots=false`.
3. `fly.toml`:

   ```toml
   app = "<název>"
   primary_region = "fra"

   [experimental]
     cmd = ["serve"]

   [env]
     NYLZO_DATA_DIR = "/data"
     NYLZO_PROXY_MODE = "fly"
     NYLZO_MIN_FREE_BYTES = "268435456"
     NYLZO_GLOBAL_DAILY_CREATES = "90"

   [[mounts]]
     source = "nylzo_data"
     destination = "/data"
     scheduled_snapshots = false

   [http_service]
     internal_port = 8080
     force_https = true
     auto_stop_machines = "off"
     auto_start_machines = true
     min_machines_running = 1

     [[http_service.checks]]
       path = "/healthz"
       interval = "15s"
       timeout = "2s"
       grace_period = "10s"
   ```

   Výchozí `NYLZO_MIN_FREE_BYTES` je 1 GiB, takže na 1GB volume by každé vytvoření zprávy vrátilo 503. Proto 5 GB volume a nižší rezerva; 8 × 90 × 5,41 MB ≈ 3,9 GB. Paměť 256 MB je minimum; dokud špička paměti pod zátěží není změřená, je bezpečnější 512 MB.
4. Pepper: `fly secrets set NYLZO_PEPPER=$(openssl rand -hex 32)`. Na Fly končí pepper v prostředí procesu i při použití `[[files]]`.
5. První nasazení: `fly deploy --ha=false --image ghcr.io/misaon/nylzo@sha256:<digest>`, pak `fly scale count 1`.
6. Doména: `fly certs add <doména>` a záznamy A (sdílená IPv4) a AAAA podle výpisu.
7. GitHub Actions: `fly tokens create deploy --expiry 2160h` (90 dní, obnovovat), secret `FLY_API_TOKEN`; job po vydání ověří podpis image přes `cosign verify` a spustí `flyctl deploy --ha=false --image ghcr.io/misaon/nylzo@sha256:<digest>`.
8. Testy před ostrým provozem: harness `tools/r13` lze nasadit stejně. `measure/headers.sh` je potřeba doplnit o podvrženou `Fly-Client-IP`. Pomalé nahrání testujte proti `nylzo`, protože echo server má ReadTimeout 120 s. Dále `measure/post-5mb.sh` a `measure/tls-pq.sh`, který ukáže, zda edge Fly vyjedná X25519MLKEM768.

## Změny serveru

Doporučené nasazení (VPS s Compose) nepotřebuje žádnou.

Fly.io potřebuje nový režim proxy:
- `NYLZO_PROXY_MODE=fly`: adresa klienta je jediná hodnota hlavičky `Fly-Client-IP`. Při chybějící hlavičce, více hodnotách nebo neplatné adrese server použije adresu spojení, stejně jako současný režim `scaleway` s `X-Real-IP`. Fly proxy hlavičku přidává s adresou klienta, jak ji vidí proxy; zda hodnotu poslanou klientem přepíše, Fly nedokumentuje a musí to ukázat test.
- Kontrola peeru přes `NYLZO_TRUSTED_PROXIES` na Fly nepůjde nastavit, protože adresy proxy nejsou zdokumentované. Ochranou je samostatná organizace (krok 1 výše).
- Validace konfigurace (`NYLZO_PROXY_MODE must be empty or scaleway`) se rozšíří o `fly`. Alternativa: obecný režim s nastavitelným názvem hlavičky, který pokryje Scaleway i Fly.

Pro ostatní platformy jen pro srovnání: Railway by šel se stávajícím režimem `scaleway`, protože ten čte `X-Real-IP`, pokud Railway hodnotu od klienta přepisuje (nezdokumentováno). Render by potřeboval režim „první položka `X-Forwarded-For`“, Koyeb a Northflank adresy svých proxy v `NYLZO_TRUSTED_PROXIES`, které nezveřejňují.

## Rizika a otevřené otázky

1. Volba mezi VPS a Fly.io je na vlastníkovi. VPS znamená správu systému (aktualizace, restarty po aktualizaci jádra, SSH, firewall), Fly.io nový kód a dvě nezdokumentované vlastnosti platformy.
2. Rozpočet: v rešerši B byl strop 100 Kč měsíčně. DigitalOcean za 6 USD stojí asi 158 Kč s DPH, za 4 USD asi 105 Kč, OVH VPS-1 asi 112 Kč s DPH při roční platbě, Hetzner CX23 asi 176 Kč s DPH. Pod 100 Kč zůstává jen Fly.io s 256 MB (odhadem 71 Kč bez daně, zda Fly účtuje DPH, jsem neověřil) a Northflank Sandbox. Potvrďte, zda strop platí i pro prototyp.
3. Jeden disk bez replikace: při selhání disku zmizí aktivní zprávy (nejvýš 7 dní provozu). Platí pro VPS i Fly. Zálohy obsahu se podle OD20 nedělají; pepper a `.env` zálohujte zvlášť, jak říká README.
4. Hetzner: zda účet vlastníka může server vytvořit, ukáže až konzole. Omezení podle FAQ dopadá na náhodnou skupinu zákazníků a podpora ho individuálně neruší.
5. Fly.io: přepis `Fly-Client-IP`, access logy proxy a jejich retence (otázka na podporu Fly), vlastník volume pro uid 65532, interakce udržovaných spojení proxy s `IdleTimeout` 30 s v `nylzo` (Caddy kvůli tomu drží spojení jen 25 s; u Fly hrozí občasné 502, ověřit testem, případně `http_options.idle_timeout`, jehož jednotka není zdokumentovaná) a zda edge vyjedná X25519MLKEM768.
6. TLS na hostingu: u VPS končí TLS na vlastním Caddy. U Fly.io vidí proxy Fly provoz v čitelné podobě včetně R, V, M a L a může podvrhnout klienta; D57 to pro prototyp připouští, README Compose to pro self-hosting zakazuje.
7. Logy Dockeru: `compose.yaml` nenastavuje `logging`. Výchozí ovladač `json-file` podle dokumentace Dockeru logy nerotuje (v této rešerši neověřeno), takže na VPS nastavte rotaci nebo ovladač `local`.
8. Northflank Sandbox jako cesta za 0 Kč: má smysl jen po testu harnessem R13 (5 MB POST, 6minutové nahrání, chování `X-Forwarded-For`, adresy load balanceru).

## Zdroje

Všechny stránky otevřené 23. 10. 2026.

| URL | Co dokládá | Stav |
| --- | --- | --- |
| https://www.cnb.cz/en/financial-markets/foreign-exchange-market/central-bank-exchange-rate-fixing/central-bank-exchange-rate-fixing/daily.txt | kurz EUR a USD | ověřeno |
| https://vercel.com/docs/functions/limitations | 4,5 MB, max. délka, paměť | ověřeno |
| https://vercel.com/docs/limits | retence runtime logů | ověřeno |
| https://vercel.com/kb/guide/docker | OCI image jako funkce, uspání po 5 min | ověřeno |
| https://vercel.com/kb/guide/vercel-daemons | Daemons bez provozu a bez trvalého úložiště | ověřeno |
| https://vercel.com/docs/plans/hobby | nekomerční použití, logy 1 h | ověřeno |
| https://vercel.com/docs/headers/request-headers | přepis `x-forwarded-for` | ověřeno |
| https://docs.fly.io/about/pricing | ceny, žádný free tier, zkušební období | ověřeno |
| https://docs.fly.io/volumes/overview | lokální NVMe, jeden volume na Machine, riziko ztráty | ověřeno |
| https://docs.fly.io/volumes/snapshots | vypnutí snapshotů | ověřeno |
| https://docs.fly.io/networking/request-headers | `Fly-Client-IP`, `X-Forwarded-For` | ověřeno, přepis nezdokumentován |
| https://docs.fly.io/reference/fly-proxy | ukončení TLS, autostart a autostop | ověřeno |
| https://docs.fly.io/reference/configuration | `http_service`, `mounts`, `files`, `experimental.cmd` | ověřeno |
| https://docs.fly.io/launch/autostop-autostart | `min_machines_running`, studený start | ověřeno |
| https://docs.fly.io/flyctl/deploy | `--image`, `--ha` | ověřeno |
| https://docs.fly.io/reference/regions | evropské regiony | ověřeno |
| https://docs.fly.io/security/tokens | deploy tokeny a jejich platnost | ověřeno |
| https://docs.fly.io/launch/continuous-deployment-with-github-actions | `FLY_API_TOKEN`, workflow | ověřeno |
| https://docs.fly.io/monitoring/logging-overview | retence logů 7 dní | ověřeno |
| https://fly.io/legal/privacy-policy | access logy aplikací neřeší | ověřeno (absence údaje) |
| https://community.fly.io/t/documentation-for-request-body-size-limit/15649 | těla se streamují | částečně |
| https://community.fly.io/t/request-timeouts-on-fly-io/5653 | 60 s bez dat | částečně |
| https://community.fly.io/t/recommended-setting-for-trust-proxy-on-express/6346 | podvržení `X-Forwarded-For` | částečně |
| https://community.fly.io/t/application-is-unable-to-create-directory-in-volume/915 | vlastník přípojného bodu volume | částečně |
| https://docs.railway.com/reference/pricing/plans | plány, ceny, limity volume | ověřeno |
| https://docs.railway.com/reference/pricing/free-trial | trial a Free plán | ověřeno |
| https://docs.railway.com/reference/volumes | jeden volume, problém s non-root UID | ověřeno |
| https://docs.railway.com/networking/public-networking/specs-and-limits | 15 min, tělo do 5 min, `X-Real-IP` | ověřeno |
| https://docs.railway.com/observability/logs | HTTP logy s IP, retence | ověřeno |
| https://render.com/pricing | Starter 7 USD, disk 0,25 USD/GB | ověřeno (z HTML) |
| https://render.com/docs/disks | disk jen u placené služby, jedna instance | ověřeno |
| https://render.com/docs/free | free služba bez disku | ověřeno |
| https://render.com/docs/web-services | `PORT`, HTTP/2 | ověřeno |
| https://render.com/docs/render-vs-vercel-comparison | 100 minut | ověřeno |
| https://render.com/docs/logging | HTTP request logy od Pro | ověřeno |
| https://feedback.render.com/features/p/send-the-correct-xforwardedfor | první IP v `X-Forwarded-For` | částečně |
| https://www.koyeb.com/docs/reference/volumes | volumes v preview | ověřeno |
| https://www.koyeb.com/docs/reference/edge-network | timeout 100 s, `x-forwarded-for` | ověřeno |
| https://www.koyeb.com/docs/faqs/pricing | free instance, Pro 29 USD | ověřeno |
| https://www.koyeb.com/pricing | plány | částečně |
| https://northflank.com/pricing | Sandbox, ceny, volumes v Sandboxu | ověřeno (z HTML) |
| https://northflank.com/docs/v1/application/databases-and-persistence/add-a-volume | jedna instance, vlastnictví svazku | ověřeno |
| https://northflank.com/docs/v1/application/network/networking-on-northflank | `X-Forwarded-For` od load balanceru | ověřeno |
| https://northflank.com/docs/v1/application/network/configure-ports | HTTP/2, TLS, vlastní doména | ověřeno |
| https://northflank.com/docs/v1/application/domains/use-path-based-routing | timeout po cestách, 408 | ověřeno |
| https://northflank.com/blog/hetzner-cloud-server-price-increases | zdražení Hetzneru 15. 6. 2026 | částečně |
| https://www.digitalocean.com/pricing/droplets | ceny Dropletů, IPv4 v ceně | ověřeno |
| https://docs.digitalocean.com/products/droplets/details/pricing/ | přenos 0,01 USD/GiB, strop 672 h | ověřeno |
| https://docs.digitalocean.com/products/app-platform/details/limits/ | App Platform bez volumes | ověřeno |
| https://www.hetzner.com/cloud | lokality | ověřeno |
| https://www.hetzner.com/cloud/cost-optimized/ | CX23 5,99 EUR, nedostupné | ověřeno |
| https://www.hetzner.com/cloud/regular-performance/ | CPX ceny, nedostupné | ověřeno |
| https://status.hetzner.com/ | „Limited availability of cloud instances“ od 26. 6. 2026 | ověřeno (jen titulek a datum) |
| https://docs.hetzner.com/cloud/general/faq/ | omezení vytváření serverů | ověřeno |
| https://www.ovhcloud.com/en-ie/vps/ | VPS-1 od 3,81 EUR | částečně |
| https://www.scaleway.com/en/docs/serverless-containers/reference-content/containers-limitations/ | timeout, dočasný disk | ověřeno |
| https://www.scaleway.com/en/pricing/virtual-instances/ | STARDUST1-S | částečně |
| https://docs.cloud.google.com/run/quotas | 32 MiB, 60 min | ověřeno |
| https://docs.cloud.google.com/run/docs/configuring/services/cloud-storage-volume-mounts | FUSE není POSIX | ověřeno |
| https://docs.cloud.google.com/free/docs/free-cloud-features | e2-micro Always Free | ověřeno |
