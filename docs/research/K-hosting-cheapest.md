# Rešerše K: nejlevnější hosting první veřejné instance

Ověřeno 24. 10. 2026 na stránkách poskytovatelů (seznam zdrojů na konci). Rešerše navazuje na `I-hosting-prototype.md` z 23. 10. 2026: co tam platí beze změny (postup nasazení na VPS, `fly.toml`, změny serveru pro Fly), tady neopakuji. Kurz ČNB vyhlášený 23. 10. 2026 (č. 205), platný i pro sobotu 24. 10.: 24,335 Kč za EUR a 21,741 Kč za USD (https://www.cnb.cz/en/financial-markets/foreign-exchange-market/central-bank-exchange-rate-fixing/central-bank-exchange-rate-fixing/daily.txt).

Ceny bez DPH jsou ceníkové ceny poskytovatele. Cena s DPH je můj výpočet s českou sazbou 21 %, kterou platí spotřebitel; firma s DIČ platí v jiném státě EU obvykle v režimu přenesení daňové povinnosti. Netcup a IONOS ukazují ceny s německou DPH 19 %, ty nejdřív přepočítám na cenu bez DPH. Jakou sazbu poskytovatel českému zákazníkovi skutečně naúčtuje, mám ověřené jen u DigitalOcean.

Stav údajů: ověřeno je citace ze stránky poskytovatele nebo z jeho veřejného API. Částečně znamená kopii stránky ve Wayback Machine, příspěvek zaměstnance na fóru nebo údaj odvozený z dat stránky. Neověřeno znamená, že oficiální zdroj jsem nenašel nebo nešel přečíst. Výpočet je moje aritmetika z ověřených cen. Měřeno je můj test z 24. 10. 2026 (TLS a DNS, sekce Měření).

## Shrnutí a doporučení

Cesta A, VPS s `deploy/compose`: nejlevnější použitelný je netcup VPS pico G11.5s v Norimberku. Má 1 vCore, 1 GB RAM, 30 GB SSD, IPv4 a IPv6 /64 a stojí 2,21 EUR měsíčně včetně německé DPH, tedy asi 1,86 EUR bez DPH a s českou DPH asi 2,25 EUR (55 Kč). Smlouva je ale na 12 měsíců placených rok dopředu (asi 27 EUR, 656 Kč s DPH) a netcup prodá jen jeden kus na zákazníka. Bez ročního závazku je nejlevnější DigitalOcean Basic za 4 USD (512 MiB, 10 GiB, 105 Kč s DPH, americká firma).

Cesta B, hosting jen pro image: trvalý svazek a právě jednu instanci a zároveň žádný vylučující limit má jen Fly.io. Machine shared-cpu-1x s 256 MB ve Frankfurtu a 5GB volume vychází na 3,28 USD za 30 dní (výpočet), tedy 71 Kč bez daně a 86 Kč s 21 % DPH, pokud ji Fly účtuje (neověřeno). Podmínky: server potřebuje nový režim `NYLZO_PROXY_MODE=fly` a test, zda Fly přepíše hlavičku `Fly-Client-IP` poslanou klientem. Edge Fly navíc X25519MLKEM768 nevyjedná (měřeno), takže AK26 by platilo jen v náhradní podobě, kterou AK26 připouští: stránka transparentnosti uvede, že post-kvantová výměna platí jen při ukončení TLS v Go nebo Caddy.

Moje doporučení: cesta A s netcupem. Je levnější, klientskou IP a X25519MLKEM768 už ověřuje CI job „Compose with Caddy“ (AK12, AK26), server nepotřebuje žádnou změnu a TLS končí na vlastním stroji. Cenou je správa systému (aktualizace, SSH, firewall) a roční závazek. Fly.io má smysl, jen pokud vlastník server spravovat nechce.

Northflank vychází cenově stejně jako Fly (2,82 až 3,28 USD, výpočet), ale nemá zdokumentovaný limit těla, výchozí timeout ani to, zda load balancer hlavičku `X-Forwarded-For` přepisuje. Jeho dokumentace o bezplatném Sandboxu říká, že „it should not be used for production applications“, a podmínky omezují komerční použití. Bez testu ho doporučit nejde.

Vyřazené: Railway, Koyeb, Scaleway Serverless Containers, Google Cloud Run, Azure Container Apps, DigitalOcean App Platform a Clever Cloud (důvody v sekci Vyřazené). Render a Sliplane jsou označené, protože klíčové vlastnosti nejsou zdokumentované nebo jsou proti požadavkům.

## Co kandidát musí splnit

1. Trvalý lokální disk s POSIX sémantikou: SQLite a bloby, fsync adresáře, hard linky. Síťové úložiště přes FUSE ani NFS bez zámků nestačí.
2. Právě jedna instance, která pořád běží, protože úklid běží v procesu každých 5 minut. Postgres a S3 adaptér zatím neexistuje.
3. Tělo do 5 410 880 B a pomalé nahrávání: při 128 kbit/s trvá 338 s (AK17).
4. Skutečná IP klienta v hlavičce, kterou klient nepodvrhne. Server věří `X-Forwarded-For` jen ze spojení z `NYLZO_TRUSTED_PROXIES` a bere nejpravější nedůvěryhodnou adresu; `NYLZO_PROXY_MODE=scaleway` čte jedinou hodnotu `X-Real-IP` (spec §9).
5. TLS 1.3 s X25519MLKEM768 (AK26) a studený start pod 3 s (AK17).
6. Podmínky, které nezakazují veřejnou anonymní službu pro sdílení zpráv a souborů, a předvídatelný postup při hlášení zneužití.

## Cesta A: VPS s Compose a Caddy

Na VPS body 1 až 5 nezávisí na poskytovateli, ale na `deploy/compose`: lokální disk, jedna instance, Caddy bez limitu těla, `X-Forwarded-For` nastavuje Caddy s pevnými adresami v `NYLZO_TRUSTED_PROXIES`, žádné uspávání. CI job „Compose with Caddy (AK12, AK17, AK26)“ ověřuje 5MiB soubor přes Caddy, podvrženou hlavičku i vyjednání X25519MLKEM768 nástrojem `tools/tlscheck` (D67). Poskytovatelé se tedy liší cenou, závazkem, lokalitou, adresami, podmínkami a dostupností.

| Poskytovatel a plán | vCPU, RAM, disk | Přenos | IPv4, IPv6 | Lokalita, sídlo firmy | Cena bez DPH | Kč s 21 % DPH | Závazek a háčky | Stav |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Oracle Always Free, Ampere A1 | 2 OCPU Arm, 12 GB, 200 GB block celkem | 10 TB | ano, ano | domovský region (Frankfurt existuje), USA | 0 | 0 | karta a telefon, nečinnou instanci může Oracle odebrat, kapacita, bez SLA | ověřeno |
| Vultr Free | 1 vCPU, 512 MB, 10 GB | neověřeno | IPv4 ano | Frankfurt, USA | 0 | 0 | jen na žádost, přijetí losem s váhou podle skóre | částečně |
| netcup VPS pico G11.5s | 1 vCore x86 sdílené, 1 GB, 30 GB SSD | bez limitu; nad 100 Mbit/s průměru za 24 h škrcení na 100 Mbit/s | ano, /64 | Norimberk, Německo | ≈1,86 EUR (2,21 s 19 % DPH) | 55 | 12 měsíců placených předem, 1 kus na zákazníka | ověřeno |
| IONOS VPS S+ | 1 vCore sdílené, 2 GB, 60 GB NVMe | neomezeno, 1 Gbit/s | ano, /80 | DE, ES, UK, US; Německo | 1,68 EUR 3 měsíce, pak 4,20 | 50, pak 124 | 12 měsíců; zákaz „Download-Services“, smluvní pokuta | ověřeno, běžná cena částečně |
| DigitalOcean Basic 4 USD | 1 vCPU sdílené, 512 MiB, 10 GiB SSD | 500 GiB | ano, ano | FRA1, AMS3, LON1; USA | 4 USD | 105 | bez závazku; zda jde tahle velikost vytvořit ve FRA1, neověřeno | ověřeno |
| OVHcloud VPS-1 | 2 vCore, 4 GB, 40 GB NVMe | neomezeno, 500 Mbit/s | ano, /128 | EU (RBX, GRA, SBG, DE, WAW, MIL, UK); Irsko a Francie | 3,81 EUR (12 měs.), 4,26 (6 měs.), 4,49 (bez závazku) | 112, 125, 132 | denní záloha povinná při objednávce | ověřeno |
| Scaleway STARDUST1-S + 10 GB + IPv4 | 1 vCPU sdílené, 1 GB, 10 GB lokální SSD | bez poplatku, 100 Mbit/s | 0,005 EUR/h, zdarma | PAR, AMS, WAW; Francie | ≈4,45 EUR (výpočet) | 131 | sklad: 24. 10. jen pl-waw-2 | ověřeno |
| Vultr Regular 5 USD | 1 vCPU, 1 GB, 25 GB SSD | 1 TB | ano, ano | fra, ams, waw, cdg, sto, mad, lhr; USA | 5 USD | 131 | web vrací 403, ceník a podmínky z Wayback | ověřeno (API), částečně |
| Contabo Cloud VPS 4 (nejmenší) | 4 vCPU, 8 GB, 100 GB SSD | fair use | ano, ano | Německo | 5,50 EUR (měsíc), 4,68 (12 měs.) | 162, 138 | zda cena obsahuje DPH, není ze stránky jasné | částečně |
| DigitalOcean Basic 6 USD | 1 vCPU sdílené, 1 GiB, 25 GiB SSD | 1 000 GiB | ano, ano | FRA1, AMS3, LON1; USA | 6 USD | 158 | bez závazku | ověřeno |
| VEDOS (dřív WEDOS) VPS ON | 1 vlákno sdílené, 1 GB, 10 GB | neověřeno | 1 Kč, zdarma | Česko | 137,70 Kč (výpočet) | 167 | přenos, závazek a podmínky neověřeny | částečně |
| Hetzner CX23 | 2 vCPU sdílené, 4 GB, 40 GB NVMe | 20 TB | v ceně, zdarma | NBG1, HEL1; Německo | 5,99 EUR | 176 | „currently unavailable“, omezení nových serverů trvá | ověřeno |
| Hetzner CAX11 (Arm), CPX02 | 2 Arm, 4 GB, 40 GB; 1 AMD, 1 GB, 20 GB | 20 TB | v ceně, zdarma | DE, FI | 6,49 EUR | 191 | CAX11 nedostupný; CPX02 bez štítku, omezení platí | ověřeno, CPX02 částečně |

### Poznámky k poskytovatelům

netcup (ověřeno, https://www.netcup.com/en/server/vps/vps-pico-g11.5s-iv-12m-nue):
- „€2.21 per month (incl. 19% VAT)“ s volbou „IPv4 + IPv6 Connectivity“; jen IPv6 je o 0,60 EUR levnější. Zřizovací poplatek 0 EUR, „Minimum contract period 12 months Billing period 12 months“.
- „Guaranteed server location: Nuremberg, Germany“, „1 vCore, KVM technology 1 GB RAM 30 GB SSD“, „This product is limited to one unit per customer.“ Zda jde o trvalou nabídku, stránka neříká.
- Dražší VPS nano (2 vCore, 2 GB, 60 GB) stojí 3,69 EUR s 19 % DPH při 6měsíčním závazku, s českou DPH asi 91 Kč (výpočet, https://www.netcup.com/en/server/vps-lite).
- Podmínky zakazují mimo jiné „hosting of warez sites, illegal MP3 downloads“ a při důvodném podezření dovolují dočasné zablokování (https://www.netcup.com/en/terms-and-conditions). Abuse oddělení pracuje v pracovní dny 10 až 18 h: „The department has a response time of 2 hours.“ (https://www.netcup.com/en/helpcenter/documentation/security/abuse-notices). Bonitu zákazníka netcup ověřuje přes mediaFinanz. Stránka zmiňuje „backup system“ a snapshoty; zda netcup sám zálohuje disk zákazníka (OD20), jsem neověřil.

DigitalOcean (ověřeno, https://www.digitalocean.com/pricing/droplets, https://docs.digitalocean.com/platform/billing/taxes/eu/):
- „Prices listed on the DigitalOcean website do not include applicable VAT charges.“ Spotřebitelům v EU DPH účtuje, DIČ ji odstraní.
- IPv4 „is included at no extra cost“, IPv6 je zdarma ve všech regionech. Platba po sekundách se stropem 672 hodin měsíčně.
- AUP (aktualizace 20. 3. 2026) zakazuje „open proxies, open mail relays, open recursive domain name servers, Tor exit nodes“ a zákazník odpovídá i za „violations of this AUP by Your Users“ (https://www.digitalocean.com/legal/acceptable-use-policy). Stránku o DSA jsem nenašel.

OVHcloud (ověřeno, https://eu.api.ovh.com/v1/order/catalog/public/vps?ovhSubsidiary=IE, https://www.ovhcloud.com/en-ie/vps/):
- Katalog: 4,49 EUR bez závazku, 4,26 EUR na 6 měsíců, 3,81 EUR na 12 měsíců, instalace 0. Pro `ovhSubsidiary=CZ` vrací katalog irskou pobočku.
- Denní záloha je zdarma a v katalogu povinná (`mandatory: true`, částečně). Zrušit jde podle zvláštních podmínek VPS jen „at the end of the Initial Subscription Term“ (https://contract.eu.ovhapis.com/1.0/pdf/Conditions_Particulieres_VPS-ie.pdf). U měsíční platby je to podle mého výkladu konec prvního měsíce. Do té doby by záloha nesla bloby a metadata, což je proti OD20.
- VPS-1 v Local Zone, mimo jiné `EU-CENTRAL-LZ-PRG` (podle kódu Praha, částečně), stojí 5,49 EUR bez závazku.
- Obecné podmínky dovolují pozastavení „immediately and without notice in the event of an emergency“ a zákazník je podle zvláštních podmínek sám poskytovatelem hostingu (https://contract.eu.ovhapis.com/1.0/pdf/contrat_genServices-ie.pdf).

Scaleway Instances (ověřeno, https://www.scaleway.com/en/pricing/virtual-instances/):
- STARDUST1-S „€0.0006 /HOUR ~€0.43 /MONTH“; „Storage (local, block) and attached public IPv4 addresses are excluded.“
- IPv4 za 0,005 EUR za hodinu (https://www.scaleway.com/en/pricing/network/); FAQ uvádí 0,004 EUR, stránky si odporují. Lokální SSD 10 GB asi 0,36 EUR měsíčně. Celkem asi 4,45 EUR (výpočet); jen s IPv6 asi 0,80 EUR, ale takový server klienti bez IPv6 neotevřou.
- Veřejné API dostupnosti v 8:47 UTC: STARDUST1-S „shortage“ ve fr-par-1, „scarce“ v nl-ams-1, „available“ v pl-waw-2 (https://api.scaleway.com/instance/v1/zones/fr-par-1/products/servers/availability, stejně pro další zóny). DEV1-S byl dostupný všude, ale s IPv4 a diskem vychází asi na 10,56 EUR.
- Obecné podmínky: „suspend all or part of the Services immediately and without prior notice“ mimo jiné po nahlášení nezákonného obsahu (https://www-uploads.scaleway.com/General_Terms_of_Services_v17072024_45d4879c08.pdf).

Hetzner (ověřeno, https://www.hetzner.com/cloud/cost-optimized/, https://status.hetzner.com/incident/0a75c7ae-3377-41dc-aabe-601063724d24):
- Ceny tabulky jsou „Price incl. IPv4“; samotná IPv4 stojí 0,50 EUR, IPv6 je zdarma (https://docs.hetzner.com/cloud/servers/primary-ips/overview/).
- Všechny plány Cost-Optimized mají „This product is currently unavailable. Please check back later.“ Incident otevřený od 26. 6. 2026, naposledy aktualizovaný 11. 9. 2026, píše, že Hetzner musel „restrict the creation of new cloud servers for both new customers and some of our existing customers“.
- Hetzner popisuje postup podle DSA: zákazník dostane lhůtu a IP se zablokuje až po ní a po ruční kontrole (https://www.hetzner.com/legal/digital-services-act/). Podobnou lhůtu má Contabo.

IONOS (ověřeno, https://www.ionos.de/server/vps, https://www.ionos.de/terms-gtc/terms-server/):
- „2€ /Monat für 3 Monate“ včetně DPH, pak 5 EUR (údaj z dat stránky, částečně), „Mindestvertragslaufzeit von 12 Monaten“.
- Serverové podmínky, bod 2.1.3, zakazují provozovat „P2P-Tauschbörsen, Download-Services oder Streaming-Dienste“, přes které se může šířit obsah chráněný autorským právem. Veřejná služba pro sdílení souborů pod to podle mého výkladu spadá. Obecné podmínky stanoví smluvní pokutu 5 050 EUR za porušení u obsahu a IONOS smí server „ohne Vorankündigung vom Netz zu trennen“. IONOS proto nedoporučuji.

Vultr (API ověřeno, zbytek částečně): `www.vultr.com` vracel 403, proto ceník, FAQ a AUP pocházejí z Wayback Machine. Veřejné API ukazuje plán za 2,50 USD jen v New Jersey a Atlantě (podle ceníku bez IPv4) a plán za 3,50 USD jen v New Jersey (https://api.vultr.com/v2/plans?type=vc2&per_page=500). V EU začíná nabídka s IPv4 na 5 USD. Bezplatný plán je v API objednatelný ve Frankfurtu, přístup ale Vultr přiděluje „on a randomized basis, incorporating a weighted score“. AUP zakazuje uzly Tor a otevřené proxy.

Oracle Always Free (ověřeno, https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm, https://www.oracle.com/cloud/free/faq/):
- A1 je teď „equivalent to 2 OCPUs and 12 GB of memory“, ne 4 a 24 jako dřív. Instance smí běžet jen v domovském regionu, který nejde změnit, a Frankfurt nemusí být při registraci nabídnutý (částečně).
- Oracle může odebrat nečinnou instanci, když je za 7 dní 95. percentil CPU, síť i paměť (u A1) pod 20 %. Málo vytížená služba jako Nylzo tak může o instanci přijít.
- „Oracle Cloud Free Tier does not include SLAs.“, karta a telefon jsou nutné, nová kapacita může trvat několik dní.

Contabo (částečně, https://contabo.com/en/vps/cloud-vps-core-4/): nejmenší plán je teď Cloud VPS 4, bez zřizovacího poplatku i při měsíční platbě. Podmínky, článek 9(4), dovolují okamžitě zablokovat servery provozující Tor; část 3 provádí DSA (https://contabo.com/en/legal/terms-and-conditions/).

## Cesta B: hosting jen pro image

| Platforma | Trvalý svazek | Jedna instance | Tělo a timeout | IP klienta | TLS, X25519MLKEM768 | Uspávání, studený start | IPv6 na edge | EU region | Cena za měsíc | Verdikt |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Fly.io | lokální NVMe 1 až 500 GB, 0,15 USD/GB | ano s `--ha=false`; při deployi výpadek | limit nezdokumentován, zaměstnanec: tělo se streamuje (částečně); 60 s bez dat zavře spojení (částečně) | `Fly-Client-IP`, přepis nezdokumentován | proxy Fly, TLS 1.2 a 1.3; MLKEM ne (měřeno); passthrough TCP zdokumentován | autostop ve výchozím stavu vypnutý; start „about 2 seconds or more“ | ano (měřeno) | ams, arn, cdg, fra, lhr | 2,43 až 3,28 USD (výpočet) | vyhovuje s podmínkami |
| Northflank | NVMe volume, služba pak „limited to 1 instance“ | ano | nezdokumentováno, timeout jde nastavit po cestách | `X-Forwarded-For` od load balanceru, přepis nezdokumentován | TLS na load balanceru; web Northflanku přes Fastly MLKEM ano, běžná služba neměřena | Sandbox neusíná | neověřeno | Frankfurt, Nizozemsko | 2,82 až 3,28 USD (výpočet), Sandbox 0 | netestováno |
| Render | disk jen u placené služby, denní snapshoty 7 dní bez zdokumentovaného vypnutí | ano | 100 min; tělo nezdokumentováno | `X-Forwarded-For`; uživatel 2024: Render hodnotu jen připojuje | Cloudflare, MLKEM ano (měřeno) | placené neusíná | ne: „Render uses IPv4.“ | Frankfurt | 7,25 až 8,25 USD (výpočet) | označeno |
| Sliplane | volume na serveru u Hetzneru, doporučený pro SQLite | ano, starý kontejner zastaví před startem nového | nezdokumentováno | nezdokumentováno | Let's Encrypt; neměřeno | neusíná | ano (dokumentace) | Německo, Finsko | 9,00 EUR | označeno |
| Railway | volume 5 GB (Hobby) | ano | tělo „must finish uploading within 5 minutes“ | `X-Real-IP`, přepis nezdokumentován | MLKEM ne (měřeno) | jen se zapnutým Serverless | ne (měřeno) | Amsterdam | 5 USD (Hobby) | vyřazeno |
| Koyeb | „only suitable for testing“ (preview) | ano | 100 s | `X-Forwarded-For`, platná jen poslední | Cloudflare, MLKEM ano (měřeno) | Deep Sleep 1 až 5 s | ano (měřeno) | fra | od 29 USD (Pro) | vyřazeno |
| Scaleway Serverless Containers | ne | při deployi běží stará i nová | 10 s až 60 min; tělo nezdokumentováno | `X-Real-IP`, stránka si odporuje | neměřeno | usne po 15 min | neověřeno | PAR, AMS, WAW, MIL | ≈6,40 EUR (výpočet) | vyřazeno |
| Google Cloud Run | jen Cloud Storage FUSE nebo NFS bez zámků | maximum lze „a few times per week“ překročit | 32 MiB přes HTTP/1, 60 min | na `run.app` nezdokumentováno | MLKEM ano (měřeno) | bez údaje | ano (měřeno) | 13 regionů v Evropě | ≈44,71 USD (výpočet) | vyřazeno |
| Azure Container Apps | Azure Files přes SMB nebo NFS (síťové) | při deployi a údržbě víc replik | 240 s | `X-Forwarded-For` připojuje zprava, adresy proxy nezveřejněné | neměřeno | bez údaje | neověřeno | západní Evropa a další | ≈10,30 až 24,61 USD (výpočet) | vyřazeno |
| DigitalOcean App Platform | „does not support volumes“ | ano | nahrávání 600 s | `do-connecting-ip` | Cloudflare, MLKEM ano (měřeno) | volitelné usínání | ano (měřeno) | AMS, LON, FRA | 5 USD | vyřazeno |
| Clever Cloud | FS Bucket je síťový a pro Docker nedostupný | | | | | | | | | vyřazeno |

### Fly.io

Ceny (ověřeno, https://docs.fly.io/about/pricing):
- shared-cpu-1x 256 MB 2,19 USD a 512 MB 3,69 USD za 30 dní v základním regionu. Zdrojový kód stránky nese přirážky regionů: fra 1,153846154, ams a arn 1,038461538, cdg a lhr 1,134615385.
- Výpočet: 256 MB ve fra 2,53 USD, s 5GB volume 3,28 USD; v ams s 1GB volume 2,43 USD; 512 MB ve fra s 5GB volume 5,01 USD. V 31denním měsíci asi o 3 % víc.
- „There’s no monthly platform fee“, karta je nutná, free tier není. Podmínky říkají jen, že poplatky jsou bez daní; zda Fly účtuje evropskou DPH, jsem neověřil (https://fly.io/legal/terms-of-service/).
- Sdílená IPv4 a IPv6 zdarma, vyhrazená IPv4 „$2/mo“, odchozí přenos v Evropě 0,02 USD za GB.

Svazek a nasazení (ověřeno, https://docs.fly.io/volumes/overview, https://docs.fly.io/volumes/snapshots): „It is not network storage.“, jeden volume na Machine, při každém deployi výpadek, snapshoty jdou vypnout (`--scheduled-snapshots=false`). Vlastní `fly.toml`, `--ha=false` a nový režim proxy popisuje rešerše I.

TLS: „The Fly proxy only supports TLSv1.2 and TLSv1.3 with strong ciphers.“ (https://docs.fly.io/networking/tls). Post-kvantovou skupinu dokumentace nezmiňuje a měření ukázalo, že ji proxy nevyjedná. Zbývají dvě cesty:
- Přijmout AK26 v náhradní podobě (prohlášení v modelu hrozeb a na stránce transparentnosti).
- TLS passthrough: „If you don’t specify handlers, we just forward TCP to your app as-is. This is useful if you want to handle TLS termination yourself“ (https://docs.fly.io/networking/services). IP klienta pak nese PROXY protokol (`proxy_proto`, výchozí verze 1). Stránka o službách uvádí raw TCP jako důvod pro vyhrazenou IPv4 za 2 USD, návod k UDP a TCP ale píše „We support public IPv6 and shared IPv4 for TCP.“ (https://docs.fly.io/networking/udp-and-tcp), takže dokumentace si odporuje. `nylzo` TLS ani PROXY protokol neumí, šlo by to jen s Caddy ve vlastním image nebo s novým kódem.

Podmínky (ověřeno, https://fly.io/legal/acceptable-use-policy/): sdílení souborů ani anonymní služby AUP nezakazuje. Zakazuje ale „URL/link shorteners, if they generate abuse complaints“, což je služba s podobným profilem zneužití jako Nylzo. Stránku o DSA Fly nemá.

### Northflank

- Ceny (ověřeno, https://northflank.com/pricing): CPU 0,01667 USD za vCPU-hodinu, paměť 0,00833 USD za GB-hodinu, disk 0,11657 USD za GiB a měsíc. Plán 0,1 vCPU a 256 MB vychází na 2,70 USD za 720 hodin, s 1 GiB 2,82 USD a s 5 GiB 3,28 USD (výpočet).
- Rozpor: ceník u Sandboxu píše „No card needed“, dokumentace „all users must add a payment method“ a „it should not be used for production applications“ (https://northflank.com/docs/v1/application/billing/pricing-on-northflank).
- Podmínky: „The Services may not be used in connection with any commercial endeavors except those that are specifically endorsed or approved by us.“ (https://northflank.com/legal/terms). Stránka AUP vrací 404.
- Volume, `X-Forwarded-For` a timeouty po cestách popisuje rešerše I beze změny. Nově: L4 load balancer s pevnou IP existuje, cenu ani předávání IP klienta dokumentace neuvádí.

### Render a Sliplane

Render (ověřeno, https://render.com/pricing.md, https://render.com/docs/disks): 7 USD za 512 MB a disk 0,25 USD za GB, plány přejmenované 26. 8. 2026 beze změny cen. Snapshoty disku drží 7 dní a vypnutí dokumentace neuvádí, což je proti OD20. Ve vlákně, ze kterého rešerše I brala „we set the first IP in the list to the real client IP“ (2021), uživatel v roce 2024 píše, že Render „does not clear or reset any passed-in X-Forwarded-For header (it only appends to it)“ (neověřeno, https://feedback.render.com/features/p/send-the-correct-xforwardedfor). Pokud to platí, první položka jde podvrhnout a adresy proxy Render nezveřejňuje.

Sliplane (ověřeno, https://sliplane.io/pricing, https://docs.sliplane.io/services/volumes/): berlínská firma spouští kontejnery na serverech u Hetzneru. „Starter 1 1 GB 20 GB 9.00 €“ bez DPH, volume patří serveru, „Sliplane stops the old container before starting its replacement.“ Limit těla, timeouty ani hlavičku s IP klienta dokumentace neuvádí. Vyjde dráž než Fly i netcup.

## Vyřazené

- Railway: „Request bodies must finish uploading within 5 minutes.“, pomalý klient potřebuje 338 s (https://docs.railway.com/networking/public-networking/specs-and-limits). Podmínky navíc zakazují „operating proxies or anonymization services“ (https://railway.com/legal/fair-use), image s UID 65532 má na volume potíže s právy a edge nemá IPv6 ani X25519MLKEM768 (měřeno).
- Koyeb: timeout 100 s a volumes „only suitable for testing“ (https://www.koyeb.com/docs/reference/edge-network, https://www.koyeb.com/docs/reference/volumes).
- Scaleway Serverless Containers: „Scaleway Serverless Containers do not currently support attaching Block Storage.“ (https://www.scaleway.com/en/docs/serverless-containers/faq/).
- Google Cloud Run: „NFS volumes are automatically mounted in no-lock mode.“ (https://docs.cloud.google.com/run/docs/configuring/services/nfs-volume-mounts), Cloud Storage FUSE „is not a fully POSIX-compliant file system“ a maximum instancí jde „a few times per week“ překročit (https://docs.cloud.google.com/run/docs/about-instance-autoscaling). Úklid v procesu navíc potřebuje fakturaci po instancích s nejméně 1 vCPU, asi 44,71 USD měsíčně (výpočet).
- Azure Container Apps: „Request time out is 240 seconds“ (https://learn.microsoft.com/en-us/azure/container-apps/ingress-overview); jediný svazek se zámky a hard linky je NFS share Azure Files, tedy síťové úložiště, a při deployi i údržbě běží víc replik.
- DigitalOcean App Platform: „App Platform does not support volumes.“ (https://docs.digitalocean.com/products/app-platform/details/limits/).
- Clever Cloud: „FS Buckets are not available for Docker applications“ (https://www.clever.cloud/developers/doc/deploy/storage/fs-bucket/).
- Vercel vyřadila rešerše I (limit těla 4,5 MB, žádný trvalý disk).

## Měření TLS a IPv6

24. 10. 2026 kolem 10:45 SELČ z připojení vlastníka v Česku, OpenSSL 4.0.3. Pro každý edge jsem spustil `openssl s_client -tls1_3 -groups X25519MLKEM768:X25519` a potom `-groups X25519MLKEM768` samotné. Když edge s nabídkou jen hybridní skupiny spojení odmítne (alert 40, handshake failure), X25519MLKEM768 nepodporuje. Testoval jsem výchozí domény platforem. Wildcard certifikát a DNS odpovídají i pro neexistující název, takže handshake ukazuje chování edge, ne konkrétní aplikace.

| Edge | Testovaný host | Skupina | IPv6 (AAAA) |
| --- | --- | --- | --- |
| Fly.io | `fly.io` (hlavička `server: Fly/…`, request id končí `-fra`) | X25519; jen MLKEM: handshake failure | ano |
| Railway | `railway.com`, náhodný `*.up.railway.app` (hlavička `x-railway-edge: ams1`) | X25519; jen MLKEM: handshake failure | ne |
| Render | `render.com`, náhodný `*.onrender.com` (Cloudflare) | X25519MLKEM768 | ne |
| Koyeb | náhodný `*.koyeb.app` (Cloudflare) | X25519MLKEM768 | ano |
| Google Cloud Run | náhodný `*.a.run.app` | X25519MLKEM768 | ano |
| DigitalOcean App Platform | aplikace `*.ondigitalocean.app` z logu Certificate Transparency (Cloudflare) | X25519MLKEM768 | ano |
| Northflank | `new.code.run`, web Northflanku na Northflanku přes Fastly | X25519MLKEM768; zda stejnou cestou jde běžná služba, neověřeno | ano |
| Scaleway Serverless Containers, Azure Container Apps | živou aplikaci jsem nenašel | neměřeno | neměřeno |
| VPS s Compose | CI job „Compose with Caddy“ (D67) | X25519MLKEM768 | podle VPS |

## Doporučené řešení pro cestu A: netcup VPS pico

- Náklady: asi 1,86 EUR měsíčně bez DPH, s 21 % DPH asi 2,25 EUR (55 Kč). Prvních 12 měsíců se platí najednou, asi 27 EUR (656 Kč) s DPH (výpočet).
- Nastavení: postup z rešerše I (Doporučené nasazení, kroky 1 až 8) platí, jen místo Dropletu se objedná netcup s IPv4 a IPv6. Na 30 GB disku dejte `NYLZO_GLOBAL_DAILY_CREATES=450`: 8 × 450 × 5,41 MB ≈ 19,5 GB zpráv, zbytek pokryje systém, image, logy a výchozí rezerva `NYLZO_MIN_FREE_BYTES` 1 GiB. Do `NYLZO_HOSTING` patří „netcup GmbH, Nuremberg, Germany“.
- Rizika:
  - Roční závazek a jeden kus na zákazníka. Když netcup nevyhoví, peníze za rok jsou pryč.
  - Jeden sdílený vCore a 1 GB RAM. Paměťová špička `nylzo` pod zátěží není změřená; rezervou je VPS nano (2 GB, asi 91 Kč s DPH, závazek 6 měsíců).
  - Jediný disk bez replikace, při jeho selhání zmizí aktivní zprávy (nejvýš 7 dní provozu), stejně jako u každého VPS v této rešerši.
  - Netcup smí server při důvodném podezření dočasně zablokovat. Na hlášení zneužití je v repozitáři `nylzo takedown` (D63) a abuse oddělení netcupu reaguje v pracovní době.
  - Jakou DPH netcup českému spotřebiteli naúčtuje, a zda zálohuje disky zákazníků, jsem neověřil.
- Záloha bez ročního závazku: DigitalOcean za 4 USD (105 Kč s DPH, 10 GiB, `NYLZO_GLOBAL_DAILY_CREATES` zhruba 120 podle rešerše I), za 6 USD (158 Kč) s 25 GiB, nebo Scaleway STARDUST1-S v pl-waw-2 (asi 131 Kč, dostupnost kolísá).

## Doporučené řešení pro cestu B: Fly.io

- Náklady: 256 MB ve fra s 5GB volume 3,28 USD za 30 dní, tedy 71 Kč bez daně a 86 Kč s 21 % DPH (výpočet). S 512 MB 5,01 USD (109 Kč, 132 Kč s DPH). Vyhrazená IPv4 pro passthrough by přidala 2 USD.
- Předpoklady: režim `NYLZO_PROXY_MODE=fly` v serveru (rešerše I, Změny serveru), samostatná organizace na Fly a test harnessem `tools/r13` před ostrým provozem: 5MB POST, pomalé nahrání, podvržená `Fly-Client-IP`.
- Rizika:
  - Edge nevyjedná X25519MLKEM768 (měřeno), AK26 jen v náhradní podobě, nebo passthrough s Caddy v image.
  - Proxy Fly vidí provoz v čitelné podobě a může podvrhnout klienta; D57 to pro prototyp připouští.
  - Přepis `Fly-Client-IP`, adresy proxy a access logy proxy Fly nedokumentuje.
  - `nylzo takedown` čte id ze stdin; jak ho na Fly spustit se vstupem v distroless image (`fly machine exec`, `fly ssh console`), jsem neověřil.
  - AUP s výhradou ke zkracovačům odkazů, na které chodí stížnosti.
  - DPH a cena v 31denním měsíci mírně nad odhadem.

## Co zbývá ověřit před provozem

Tato rešerše nic nenasazovala ani neobjednávala. Před ostrým provozem zbývají testy z rešerše I: na VPS kroky 6 a 7 doporučeného nasazení, na Fly harness `tools/r13` doplněný o podvrženou `Fly-Client-IP`.

## Rozhodnutí pro vlastníka

1. Cesta A (netcup, 55 Kč měsíčně, správa serveru) nebo cesta B (Fly.io, 71 až 86 Kč měsíčně, nový kód a náhradní AK26). Doporučuji A.
2. U cesty A: roční závazek u netcupu, nebo měsíční platba bez závazku u DigitalOcean (105 nebo 158 Kč s DPH).
3. Účet na soukromou osobu s 21 % DPH, nebo na firmu s DIČ v režimu přenesení daňové povinnosti.
4. U cesty B: přijmout AK26 v náhradní podobě, nebo chtít TLS passthrough (Caddy ve vlastním image nebo TLS a PROXY protokol v `nylzo`, plus nejspíš 2 USD za IPv4).
5. Zda zkoušet bezplatné nabídky (Oracle Always Free, Vultr Free). Nedoporučuji je jako hlavní hosting: Oracle může nečinnou instanci odebrat a Vultr přiděluje přístup losem.
6. Rozpočet: strop 100 Kč z rešerše B splní netcup i Fly s 256 MB; Fly s 512 MB, DigitalOcean (105 a 158 Kč) i OVH (od 112 Kč) ho překročí.

## Změny proti rešerši I

- Doporučený VPS je netcup místo DigitalOcean za 6 USD; rešerše I netcup, IONOS, Contabo, Vultr ani VEDOS neprověřovala.
- Hetzner: omezení nových serverů trvá, incident naposledy aktualizovaný 11. 9. 2026.
- Fly.io: cena ve fra je spočtená z přirážek regionů na stránce, ne odhadnutá; edge nevyjedná X25519MLKEM768 (měřeno); passthrough TCP je zdokumentovaný.
- Render: vlákno o `X-Forwarded-For` obsahuje novější uživatelský test, který odporuje vyjádření z roku 2021; edge nemá IPv6.
- Northflank: dokumentace Sandboxu odporuje ceníku (karta, produkční použití), podmínky omezují komerční použití.
- Railway: podmínky zakazují „anonymization services“.
- Oracle A1 v Always Free má teď 2 OCPU a 12 GB.

## Zdroje

Všechny stránky otevřené 24. 10. 2026.

| URL | Co dokládá | Stav |
| --- | --- | --- |
| https://www.cnb.cz/en/financial-markets/foreign-exchange-market/central-bank-exchange-rate-fixing/central-bank-exchange-rate-fixing/daily.txt | kurz EUR a USD | ověřeno |
| https://www.netcup.com/en/server/vps/vps-pico-g11.5s-iv-12m-nue | VPS pico: cena, závazek, Norimberk, 1 kus, škrcení | ověřeno |
| https://www.netcup.com/en/server/vps-lite | VPS nano | ověřeno |
| https://www.netcup.com/en/server/vps | lokality netcupu | ověřeno |
| https://www.netcup.com/en/terms-and-conditions | zakázaný obsah, blokace, mediaFinanz | ověřeno |
| https://www.netcup.com/en/helpcenter/documentation/security/abuse-notices | abuse oddělení, 2 hodiny | ověřeno |
| https://www.netcup.com/en/contact/legal-notice | sídlo netcup GmbH | ověřeno |
| https://www.ionos.de/server/vps | VPS S+, cena, IPv4, IPv6 | ověřeno, běžná cena částečně |
| https://www.ionos.de/terms-gtc/terms/ | 12 měsíců, smluvní pokuta | ověřeno |
| https://www.ionos.de/terms-gtc/terms-server/ | „Download-Services“, odpojení bez oznámení | ověřeno |
| https://www.ionos.de/impressum | kontakt podle DSA | ověřeno |
| https://www.digitalocean.com/pricing/droplets | plány 4 a 6 USD, IPv4 v ceně | ověřeno |
| https://docs.digitalocean.com/products/droplets/details/pricing/ | platba po sekundách, strop 672 h | ověřeno |
| https://docs.digitalocean.com/platform/billing/taxes/eu/ | DPH v EU | ověřeno |
| https://docs.digitalocean.com/products/networking/ipv6/details/ | IPv6 zdarma | ověřeno |
| https://docs.digitalocean.com/products/droplets/details/availability/ | regiony Basic | ověřeno |
| https://www.digitalocean.com/legal/acceptable-use-policy | Tor, proxy, odpovědnost za uživatele | ověřeno |
| https://eu.api.ovh.com/v1/order/catalog/public/vps?ovhSubsidiary=IE | ceny VPS-1 podle závazku, záloha, datacentra | ověřeno, kódy datacenter částečně |
| https://www.ovhcloud.com/en-ie/vps/ | VPS-1, ceny bez DPH, přenos | ověřeno |
| https://contract.eu.ovhapis.com/1.0/pdf/Conditions_Particulieres_VPS-ie.pdf | zrušení zálohy, zákazník jako hosting | ověřeno |
| https://contract.eu.ovhapis.com/1.0/pdf/contrat_genServices-ie.pdf | pozastavení bez oznámení, sídlo | ověřeno |
| https://www.scaleway.com/en/pricing/virtual-instances/ | STARDUST1-S, DEV1-S | ověřeno |
| https://www.scaleway.com/en/pricing/network/ | IPv4 0,005 EUR/h | ověřeno, FAQ odporuje |
| https://api.scaleway.com/instance/v1/zones/fr-par-1/products/servers/availability | sklad Stardust (i nl-ams-1, pl-waw-2) | ověřeno, okamžitý stav |
| https://www-uploads.scaleway.com/General_Terms_of_Services_v17072024_45d4879c08.pdf | pozastavení bez oznámení | ověřeno |
| https://www.hetzner.com/cloud/cost-optimized/ | CX23, CAX11, nedostupné | ověřeno |
| https://www.hetzner.com/cloud/regular-performance/ | CPX02 | ověřeno |
| https://status.hetzner.com/incident/0a75c7ae-3377-41dc-aabe-601063724d24 | omezení nových serverů | ověřeno |
| https://docs.hetzner.com/cloud/servers/primary-ips/overview/ | cena IPv4, IPv6 zdarma | ověřeno |
| https://www.hetzner.com/legal/digital-services-act/ | postup podle DSA | ověřeno |
| https://api.vultr.com/v2/plans?type=vc2&per_page=500 | plány a lokality | ověřeno |
| https://api.vultr.com/v2/regions/fra/availability?type=vc2 | dostupnost ve Frankfurtu včetně free plánu | ověřeno |
| https://docs.vultr.com/platform/billing/faq | ceny bez daně | ověřeno |
| https://web.archive.org/web/20261017183308/https://www.vultr.com/pricing/ | ceník, plán jen s IPv6 | částečně |
| https://web.archive.org/web/20261016173938/https://www.vultr.com/resources/faq/ | pravidla free plánu | částečně |
| https://web.archive.org/web/20260817184054/https://www.vultr.com/legal/use-policy/ | AUP | částečně |
| https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm | Always Free zdroje, odebrání nečinných instancí | ověřeno |
| https://www.oracle.com/cloud/free/faq/ | SLA, karta, kapacita, regiony při registraci | ověřeno, regiony částečně |
| https://contabo.com/en/vps/cloud-vps-core-4/ | Cloud VPS 4, ceny podle závazku | ověřeno, DPH neověřeno |
| https://contabo.com/en/legal/terms-and-conditions/ | Tor, DSA | ověřeno |
| https://vedos.cz/vps-on/ | jednotkové ceny VPS ON | ověřeno |
| https://vedos.cz/vps-on/doplnkove-sluzby/ | IPv4 za 1 Kč | ověřeno |
| https://docs.fly.io/about/pricing | ceny, přirážky regionů, IPv4, bez free tieru | ověřeno |
| https://docs.fly.io/volumes/overview | lokální NVMe, jeden volume na Machine | ověřeno |
| https://docs.fly.io/networking/tls | TLS 1.2 a 1.3 na proxy | ověřeno |
| https://docs.fly.io/networking/services | passthrough TCP, `proxy_proto`, vyhrazená IPv4 | ověřeno |
| https://docs.fly.io/networking/udp-and-tcp | TCP na sdílené IPv4 a IPv6 | ověřeno, odporuje předchozímu |
| https://docs.fly.io/launch/autostop-autostart | studený start | ověřeno |
| https://fly.io/legal/acceptable-use-policy/ | AUP | ověřeno |
| https://fly.io/legal/terms-of-service/ | poplatky bez daní | ověřeno |
| https://northflank.com/pricing | ceny, Sandbox | ověřeno |
| https://northflank.com/docs/v1/application/billing/pricing-on-northflank | Sandbox s kartou, ne pro produkci | ověřeno |
| https://northflank.com/legal/terms | omezení komerčního použití | ověřeno |
| https://northflank.com/docs/v1/application/network/configure-load-balancers | L4 load balancer | ověřeno |
| https://render.com/pricing.md | Render ceny | ověřeno |
| https://render.com/docs/disks | disk, snapshoty | ověřeno |
| https://render.com/docs/custom-domains | IPv4, Cloudflare | ověřeno |
| https://feedback.render.com/features/p/send-the-correct-xforwardedfor | `X-Forwarded-For` 2021 a 2024 | částečně, neověřeno |
| https://sliplane.io/pricing | Sliplane cena | ověřeno |
| https://docs.sliplane.io/services/volumes/ | volume pro SQLite | ověřeno |
| https://docs.sliplane.io/services/deploying-a-service/ | jedna instance při deployi | ověřeno |
| https://docs.railway.com/networking/public-networking/specs-and-limits | tělo do 5 min | ověřeno |
| https://railway.com/legal/fair-use | zákaz anonymizačních služeb | ověřeno |
| https://www.koyeb.com/docs/reference/edge-network | timeout 100 s | ověřeno |
| https://www.koyeb.com/docs/reference/volumes | volumes v preview | ověřeno |
| https://www.scaleway.com/en/docs/serverless-containers/faq/ | bez Block Storage | ověřeno |
| https://www.scaleway.com/en/docs/serverless-containers/reference-content/request-headers/ | `X-Real-IP`, rozporné věty | ověřeno |
| https://www.scaleway.com/en/pricing/serverless/ | ceny Serverless Containers | ověřeno |
| https://docs.cloud.google.com/run/docs/configuring/services/nfs-volume-mounts | NFS bez zámků | ověřeno |
| https://docs.cloud.google.com/run/docs/configuring/services/cloud-storage-volume-mounts | FUSE není POSIX | ověřeno |
| https://docs.cloud.google.com/run/docs/about-instance-autoscaling | překročení maxima instancí | ověřeno |
| https://cloud.google.com/run/pricing | ceny Cloud Run | ověřeno, region částečně |
| https://learn.microsoft.com/en-us/azure/container-apps/ingress-overview | 240 s, `X-Forwarded-For` | ověřeno |
| https://learn.microsoft.com/en-us/azure/container-apps/storage-mounts | typy svazků | ověřeno |
| https://prices.azure.com/api/retail/prices | ceny Container Apps v westeurope | ověřeno |
| https://docs.digitalocean.com/products/app-platform/details/limits/ | bez volumes, Cloudflare | ověřeno |
| https://www.clever.cloud/developers/doc/deploy/storage/fs-bucket/ | FS Bucket | ověřeno |
| https://crt.sh/?q=%25.ondigitalocean.app | název aplikace pro měření TLS | ověřeno |
