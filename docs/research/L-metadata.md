# Rešerše L: metadata na ukradeném serveru

Ověřeno 25. 10. 2026 (seznam zdrojů na konci). Stav zdroje: ověřeno znamená přečteno na stránce, částečně znamená nepřímý nebo neúplný údaj, nenalezeno znamená, že oficiální zdroj jsem nenašel. Tvrzení, na kterých návrh stojí, jsem ověřil druhým čtením stránky nebo v místní instalaci Go 1.27.3.

Rozhodnutí vlastníka 25. 10. 2026 (D101): zabezpečit jen aplikaci, a to co nejvíc. Vrstvu 2 (šifrovaný svazek) a vrstvu 3 (síť) neřešíme; vrstvu 1 rozšiřuje plán `docs/plans/2026-10-25-sealed-records.md`.

## Shrnutí

- Ukradenou kopii dat (disk, zálohu, snímek i s konfigurací) jde udělat pro útočníka nečitelnou až na počet zpráv a jejich zaokrouhlené velikosti. Potřebuje to dvě vrstvy: zapečetěné záznamy v aplikaci a šifrovaný datový svazek s klíčem mimo server.
- Server, který útočník ovládá za běhu, nečitelný být nemůže. Aby počítal otevření, musí vědět, kterou zprávu vydává, a vidí IP adresy a časy požadavků. Hlavně ale může poslat upravený webový klient (Z2), proti kterému chrání jen CLI nebo místní klient (OD27).
- Návrh vychází z jednoho pravidla: co server po restartu přečte sám, přečte i zloděj s kopií disku. Klíč k metadatům proto musí přicházet zvenku, od držitele odkazu v každém požadavku nebo od provozovatele či jiného stroje při startu.

## Co ukradená kopie prozradí dnes

Podle `spec/protocol.md` §13: id každé zprávy (v tabulce i v názvu souboru s obálkou), politiku, stav, časy na minutu, počty otevření a chybných passphrase, zaokrouhlenou velikost a hlášení zneužití i s e-maily oznamovatelů. Obsah ani klíče ne.

## Návrh

### Vrstva 1: zapečetěné záznamy (aplikace, klient beze změny)

- Místo id server ukládá `locator = SHA-256("nylzo/v2/locator" || id)`, i v názvu bloba, v čítačích a v hlášeních. Id má 128 náhodných bitů, takže z lokátoru ho nikdo nezjistí.
- Všechno ostatní o zprávě (politika, `kdf_id`, flags, stav, časy, počty, mac ověřovače, lease, počet hlášení, velikost) je jeden záznam pevné délky zašifrovaný AES-256-GCM náhodným klíčem záznamu. Ten server uloží dvakrát zabalený, klíčem odvozeným HKDF z `pk_R` a klíčem odvozeným z `pk_M`. Veřejné klíče server neukládá (dnes drží jen jejich HMAC) a dostává je jen při vytvoření a v podepsaném požadavku. Záznam tedy otevře jen na dobu požadavku držitele odkazu nebo správcovského klíče. Úspěšné rozbalení zároveň nahradí dnešní porovnání macu.
- Úklid musí bez klíče vědět, kdy smazat blob a kdy řádek. V řádku proto zůstanou čitelné jen náhodný odkaz na blob a dva časy úklidu zaokrouhlené nahoru na interval úklidu. Konec zprávy do záznamu zapisuje jen požadavek s klíčem, úklid jen maže podle těch dvou časů.
- Hlášení zneužití server zašifruje veřejným klíčem provozovatele: HPKE z `crypto/hpke` (Go 1.26) s hybridní post-kvantovou výměnou MLKEM768X25519, která vychází z návrhu IETF, zatím ne z RFC. Soukromý klíč zůstane na počítači provozovatele a hlášení přečte `nylzo`. Server hlášení zapíše, ale přečíst je neumí.
- Pepper zůstává pro tokeny id, výzvy a mac `pk_V` uvnitř záznamu.
- Cena: změna jen na serveru (schéma 5, úložiště, autorizace, úklid, hlášení, spec §5.2, §12 a §13 a přísnější `TestStolenServerRevealsNothing`). Klient ani protokol se nemění. Vydání zatím neexistuje, takže migrace dat není potřeba. Požadavek navíc stojí HKDF a dvě operace AES-GCM, tedy mikrosekundy. Neznámý lokátor musí stát stejnou práci jako špatný klíč (AK21, AK22).
- Hranice: běžící server dostane `pk_R` v požadavku a záznam té zprávy otevře, stejně jako edge poskytovatele (T13).

### Vrstva 2: šifrovaný datový svazek s klíčem mimo server (provoz na VPS)

- Datový adresář leží na svazku LUKS a klíč je vázaný na síť přes Clevis a Tang (NBDE). Tang běží na jiném stroji, u jiného poskytovatele nebo doma, a firewall ho pustí jen z adresy VPS. Druhou možností je ruční odemčení po restartu přes SSH.
- Pokryje, na co aplikace nedosáhne: `ctime` souborů, který server nastavit nemůže, uvolněné bloky, WAL, logy, časy úklidu a snímky a zálohy poskytovatele.
- Hranice: Tang klienty neověřuje, takže kdo má kopii disku a dostane se k Tangu, svazek odemkne. Proto firewall, log přístupů na Tangu a klíče Tangu nikdy na stejném médiu. Za běhu je svazek odemčený a hypervisor vidí paměť. Na hostingu bez vlastního serveru (Zerops, Fly) tahle vrstva nejde a zůstane jen vrstva 1.
- Cena: druhý stroj a jeho správa. Když Tang po restartu není dostupný, server nenastartuje, dokud ho provozovatel neodemkne ručně.

### Vrstva 3: síť a běžící server

- IP adresa: Tor onion service je levná a jde hned. Na VPS poběží Tor s `HiddenServicePort` na 127.0.0.1 a web pošle hlavičku `Onion-Location`. Uživatel Tor Browseru pak serveru svou IP neprozradí. Doporučuji jako volitelný krok.
- OHTTP (RFC 9458) by IP skrylo všem, ale relay nesmí provozovat ten, kdo provozuje server. Cloudflare relay nabízí jen firmám v uzavřené betě, Fastly přes obchodní oddělení a veřejný relay jsem nenašel. Pro tuto instanci teď nejde. Apple Private Cloud Compute ho používá s relayi od Cloudflare a Fastly.
- Kterou zprávu kdo čte: PIR to skryje, ale server kvůli každému dotazu projde celou databázi a PIR umí jen čtení. Systém, který by skrytý přístup spojil se spotřebováním otevření, jsem nenašel. Jednorázovost vynucená serverem jde proti skrytí přístupu.
- Důvěrné VM (SEV-SNP, TDX) a Nitro Enclaves: nejlevnější v EU vychází na 53 až 76 USD měsíčně (AWS, Azure), Scaleway ani Hetzner je nenabízí. Útoky z let 2025 a 2026 (Battering RAM, TEE.fail, DDRop s interposerem za 159 USD) je prolomí s krátkým fyzickým přístupem, tedy z pozice poskytovatele. Prohlížeč navíc atestaci neověří, takže Z2 platí dál. Nedoporučuji.

### Co kdo uvidí

| Útočník | Dnes | Po vrstvě 1 | Po vrstvách 1 a 2 |
|---|---|---|---|
| Kopie disku, záloha nebo snímek i s konfigurací | id, politika, stav, časy na minutu, počty, velikosti, hlášení s e-maily | počet zpráv, zaokrouhlené velikosti, časy úklidu anonymních řádků, počet hlášení za den | jen velikost svazku |
| Čtení souborů běžícího serveru (chyba v kódu, zálohovací agent) | totéž | jako po vrstvě 1 | jako po vrstvě 1, svazek je za běhu odemčený |
| Útočník, který ovládá běžící server nebo hosting | vše včetně IP; může poslat upravený klient | totéž, jen záznamy otevře až s příchodem držitele odkazu | totéž; Tor skryje IP uživatelům Tor Browseru |

### Srovnání s podobnými nástroji

Žádný z prověřených nástrojů metadata zprávy nešifruje. PrivateBin drží expiraci a příznak burn-after-reading čitelně, Password Pusher expiraci, počet zobrazení, čas vytvoření a v auditním logu IP a user agent. Onetime Secret šifruje obsah na serveru a účtenku drží 14 dní. Yopass s Memcached drží všechno jen v paměti, takže restart smaže metadata i zprávy. Signal podle zveřejněných odpovědí na soudní příkazy vydal jen čas registrace a posledního připojení.

## Doporučení pro vlastníka (OD28)

1. Vrstvu 1 udělat teď, před prvním vydáním, jako jedno PR jen na serveru.
2. O vrstvě 2 rozhodnout spolu s hostingem. Na VPS LUKS s Tangem na druhém stroji, nebo s ručním odemčením.
3. Tor onion service přidat jako volitelný krok po spuštění instance.
4. OHTTP a důvěrné VM teď nedělat. K OHTTP se vrátit, až bude dostupný relay pro malé projekty.

## Zdroje

- Go `crypto/hpke`: RFC 9180, `Seal` a `Open` nad veřejným klíčem, KEM včetně `MLKEM768X25519`. Ověřeno v místní instalaci (`go doc crypto/hpke`, Go 1.27.3) a v https://go.dev/doc/go1.26 a https://pkg.go.dev/crypto/hpke. Ověřeno.
- Tang README, https://github.com/latchset/tang: Tang „doesn't require TLS or authentication“ a nikdy nevidí klíč klienta; klíče Tangu nemají ležet na stejném médiu jako chráněná data. Ověřeno.
- Red Hat OpenShift, NBDE, https://docs.redhat.com/en/documentation/openshift_container_platform/4.15/html/security_and_compliance/network-bound-disk-encryption-nbde: zloděj potřebuje disky i síťový přístup k Tangu. Ověřeno. Návod pro jedno VPS u poskytovatele: nenalezeno.
- Boneh a Lipton, A Revocable Backup System, USENIX Security 1996, https://www.usenix.org/conference/6th-usenix-security-symposium/revocable-backup-system: šifra slouží k tomu, aby systém informaci „zapomněl“. Ověřeno.
- NIST SP 800-88 Rev. 2 (září 2025), https://csrc.nist.gov/pubs/sp/800/88/r2/final: kryptografické vymazání je metoda purge za podmínky, že data nikdy neležela na médiu nešifrovaná a všechny kopie klíče jdou zničit. Ověřeno.
- Linux `utimensat(2)` a `inode(7)`, https://man7.org/linux/man-pages/man2/utimensat.2.html: každé nastavení časů nastaví `ctime` na aktuální čas; jiné nastavení `ctime` jsem nenašel. Částečně.
- Amazon S3, https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingMetadata.html: `Last-Modified` uživatel změnit nemůže. Ověřeno.
- RFC 9458, https://www.rfc-editor.org/rfc/rfc9458.html: „Oblivious HTTP has limited applicability“, relay nesmí provozovat stejný subjekt jako gateway. Ověřeno.
- Cloudflare OHTTP Relay, https://developers.cloudflare.com/ohttp-relay/: Enterprise, uzavřená beta pro vybrané firmy. Ověřeno. Fastly, https://docs.fastly.com/products/oblivious-http-relay: přístup přes obchodní oddělení, ceny neuvádí. Ověřeno. Veřejný relay pro třetí strany: nenalezeno.
- Knihovny OHTTP: ohttp-ts 0.6.1 z 30. 9. 2026 (https://github.com/thibmeu/ohttp-ts, podle README neauditovaná), cloudflare/privacy-gateway-server-go v0.0.4. Ověřeno.
- Tor onion services, https://community.torproject.org/onion-services/setup/ a https://community.torproject.org/onion-services/advanced/onion-location/. Ověřeno.
- Apple Private Cloud Compute, https://security.apple.com/documentation/private-cloud-compute/requestflow a https://security.apple.com/documentation/private-cloud-compute/pir: relay od třetí strany, PIR projde pro každý dotaz celou databázi a umí jen čtení. Ověřeno.
- PIR: SimplePIR (https://eprint.iacr.org/2022/949), YPIR (https://eprint.iacr.org/2024/270), Respire (https://eprint.iacr.org/2024/1165). Ověřeno. Spojení skrytého přístupu se spotřebováním: nenalezeno. Signal SVR3 (https://usenix.org/system/files/osdi24-connell.pdf) maže klíče po daném počtu pokusů v enklávách a sám uvádí, že enklávy vzory přístupu do paměti neskrývají. Ověřeno.
- Důvěrné VM: AWS SEV-SNP (https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/sev-snp.html), Nitro Enclaves (https://docs.aws.amazon.com/enclaves/latest/user/nitro-enclave.html), Azure (https://learn.microsoft.com/en-us/azure/confidential-computing/virtual-machine-options). Ověřeno. Ceny jsou výpočet z ceníků pro 730 hodin. Scaleway ENT1 je historická nabídka, Hetzner nic: nenalezeno.
- Útoky: Battering RAM (https://batteringram.eu/), WireTap (https://wiretap.fail/), TEE.fail (https://tee.fail/), DDRop (https://ddropattack.eu/, CCS 2026, materiál za 159 USD). Ověřeno.
- Podobné nástroje: Yopass (https://github.com/jhaals/yopass), PrivateBin (https://github.com/PrivateBin/PrivateBin/wiki/Encryption-format), Password Pusher (https://github.com/pglombardo/PasswordPusher), Onetime Secret (https://github.com/onetimesecret/onetimesecret). Ověřeno. Signal, https://signal.org/bigbrother/. Ověřeno.
