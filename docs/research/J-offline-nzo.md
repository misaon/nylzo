# Rešerše J: offline zpráva v souboru `.nzo`

Datum rešerše: 23. 10. 2026. Zadání: vlastník chce, aby Nylzo umělo zašifrovat zprávu s volitelnou přílohou do jednoho souboru `.nzo`, který se offline přenese na USB a příjemcova binárka `nylzo` ho otevře. Tato zpráva nápad ověřuje, upravuje a navrhuje formát, CLI a rozhodnutí. Vychází z `docs/designs/nylzo-architecture-research.md` (model hrozeb, „Kvantové hrozby pro tento návrh“, OD19, OD26, D16, sekce CLI) a ze `spec/protocol.md`.

Stav ověření u každého tvrzení: „ověřeno“ znamená, že jsem zdroj 23. 10. 2026 otevřel a tvrzení v něm našel; „částečně“ znamená, že zdroj potvrzuje jen část nebo jde o sekundární zdroj; „neověřeno“ znamená, že primární zdroj nebyl dostupný; „úvaha“ je moje technická úvaha bez zdroje. Čísla v hranatých závorkách odkazují na seznam zdrojů v kapitole 15.

---

## 1. Shrnutí a doporučení

Nápad je proveditelný a dává smysl jako druhý způsob doručení vedle odkazu. Celé šifrování zvládne standardní knihovna Go 1.27.3 (crypto/hpke, crypto/mlkem, crypto/mldsa, crypto/ecdsa, crypto/aes, crypto/hkdf, crypto/sha512) a balíček golang.org/x/crypto/argon2 z modulu, který už je v `go.mod`. Žádný nový Go modul není potřeba.

Tvrzení „ani NSA ho nerozšifruje“ nejde dokázat a nesmí se publikovat. Pravdivě jde říct, že soubor používá parametry, které NSA v CNSA 2.0 uvádí pro národní bezpečnostní systémy (AES-256, ML-KEM-1024, ML-DSA-87, SHA-512), a že útok přímo na šifru je podle dnešních veřejných znalostí mimo dosah. Skutečné útoky vedou jinudy: slabá passphrase, napadený počítač odesílatele nebo příjemce, podvržený veřejný klíč při výměně, metadata na USB, zbytky plaintextu na disku a chyby v implementaci.

Doporučený návrh:

- Vlastní kontejner `.nzo` v1 složený jen ze standardních stavebních bloků: HPKE (RFC 9180) s hybridním KEM MLKEM1024-P384 z draft-ietf-hpke-pq, náhodný 256bitový klíč souboru, obsah v blocích po 64 KiB šifrovaných AES-256-GCM s čítačem a příznakem posledního bloku (stejná struktura jako payload v age, jen s AES), HKDF-SHA-512 a HMAC-SHA-512 pro hlavičku. Jedna pevná sada algoritmů na verzi formátu, žádné vyjednávání.
- Výchozí způsob předání klíče je veřejný klíč příjemce („identita“). Příjemce si ji vygeneruje jednou a odesílateli ji předá, oba porovnají otisk z očí do očí nebo jiným ověřeným kanálem. V tomto režimu je podpis odesílatele povinný: složený podpis ML-DSA-87 a ECDSA P-384 podle draft-ietf-lamps-pq-composite-sigs, uložený uvnitř šifrovaného obsahu a vázaný na hlavičku a na seznam příjemců.
- Další režimy: náhodný 256bitový klíč souboru předaný druhým kanálem (obdoba fragmentu odkazu v1) a vygenerovaná passphrase z nejméně 10 slov seznamu EFF (zhruba 129 bitů) s Argon2id 2 GiB. Volitelně kombinace klíče příjemce s druhým tajemstvím.
- Jednorázové klíče příjemce jako jediná poctivá náhrada „jednorázového otevření“: příjemce po otevření soukromý klíč smaže a soubor pak nepřečte nikdo, ani on sám, pokud klíč ani plaintext nikde jinde nezůstaly.
- Padding PADMÉ s minimem 64 KiB, název a typ přílohy jen uvnitř šifry, meta JSON z protokolu v1 se stejným schématem a kanonickým kódováním.
- Nejdřív jen CLI. Prohlížeč později a nejspíš jen pro režim s klíčem souboru, protože ML-KEM a ML-DSA ve Web Crypto nejsou a dostupná JS knihovna nemá nezávislý audit.

Hlavní alternativou je formát age v1.3. Je zralý, má specifikaci, testovací vektory a nezávislou implementaci v Rustu a post-kvantového příjemce má od v1.3.0 (27. 12. 2025). Klíč souboru má ale 128 bitů, obsah šifruje ChaCha20-Poly1305, post-kvantová část je ML-KEM-768, nemá podpis odesílatele ani padding a přidal by moduly filippo.io/age, filippo.io/hpke a filippo.io/nistec. Měřítko CNSA 2.0, které si vlastník zvolil, tedy nesplní. Vlastní kontejner parametry splní a závislost nepřidá. Zaplatí se to novou specifikací, která před vydáním potřebuje testovací vektory, fuzzing a nezávislé posouzení, stejně jako protokol v1.

Odhad pracnosti je 20 až 26 člověkodnů bez externího posouzení (kapitola 13). Rozhodnutí pro vlastníka jsou v kapitole 12.

---

## 2. Co lze slíbit a co ne

### 2.1 Měřítko: CNSA 2.0

Ověřeno:

- NSA na stránce Post-Quantum Cybersecurity Resources odkazuje pro výběr kvantově odolných algoritmů na CNSS Policy 15 „released March 4, 2025“ a na FAQ k CNSA 2.0 ve verzi 2.2 [1].
- Rozcestník PQC na nsa.gov uvádí, že podle Executive Order 14412 a CNSA „NSA requires that all new commercial NSS support quantum-resistant algorithms by 2027“ a starší systémy se mají vyřadit do roku 2030 [2].
- Seznam algoritmů: „ML-DSA-87 and ML-KEM-1024 together with SHA-384, SHA-512, AES-256, LMS, and XMSS comprise the CNSA Suite 2.0“. Cituji z draft-becker-cnsa2-tls-profile-05 (19. 7. 2026), jehož autoři jsou nebo byli z NSA Center for Cybersecurity Standards [4]. ML-KEM-768 ani ML-DSA-65 v seznamu nejsou.
- Executive Order 14412 z 22. 6. 2026 (Federal Register 25. 6. 2026) ukládá převést systémy s vysokou hodnotou a vysokým dopadem mimo NSS na PQC pro ustavení klíče do 31. 12. 2030 a pro podpisy do 31. 12. 2031 [5]. Plán NIST na vyřazení kvantově zranitelných algoritmů, NIST IR 8547, je stále návrh z 12. 11. 2024 [7].

Neověřeno: primární dokumenty CNSA 2.0 na media.defense.gov (advisory a FAQ v2.2) vrátily 403 přes WebFetch, curl i headless prohlížeč gstack, a to i po prokliku z nsa.gov [3]. Tabulka termínů podle kategorií (podepisování softwaru, prohlížeče a servery, operační systémy) a postoj NSA k hybridním schématům tedy ověřené nejsou. Sekundární zdroje se v postoji k hybridům rozcházejí. Formulaci „pro všechny stupně utajení“ jsem našel jen v sekundárních zdrojích.

Jak se navržené složky `.nzo` mají k seznamu CNSA 2.0:

| Složka `.nzo` | Návrh | V seznamu CNSA 2.0 |
|---|---|---|
| Šifrování obsahu | AES-256-GCM | ano (AES-256) |
| KEM, post-kvantová část | ML-KEM-1024 | ano |
| KEM, klasická část | ECDH P-384 | ne; P-384 patří do CNSA 1.0 (úvaha, v této rešerši neověřeno) |
| Kombinátor hybridu | SHA3-256 a SHAKE-256 podle draft-irtf-cfrg-concrete-hybrid-kems [20] | ne, seznam uvádí SHA-384 a SHA-512 |
| Odvození klíčů, MAC hlavičky | HKDF-SHA-512, HMAC-SHA-512 | ano (SHA-512) |
| Podpis, post-kvantová část | ML-DSA-87 | ano |
| Podpis, klasická část | ECDSA P-384 | ne |
| Klíč z passphrase | Argon2id | ne; CNSA heslové KDF neřeší (úvaha) |

Z toho plyne poctivá formulace: post-kvantové a symetrické části používají parametry ze seznamu CNSA 2.0. Nelze říct „splňuje CNSA 2.0“, „schváleno NSA“ ani „certifikováno“. Produkt pro NSS potřebuje validaci a validovaný kryptografický modul (úvaha), a balíček crypto/mldsa podle dokumentace v certifikovaném FIPS modulu Go v1.0.0 nefunguje, je až ve verzi modulu v1.26.0 [15].

### 2.2 Kvantový počítač

Grover a AES-256: NIST v odpovědi na otázku, zda zdvojnásobit délku klíče AES, píše, že Groverův algoritmus pravděpodobně proti AES přinese malou nebo žádnou výhodu a že AES 192 a AES 256 zůstanou bezpečné velmi dlouho, protože Groverův algoritmus se těžko paralelizuje [6] (ověřeno). 256bitový klíč souboru a AES-256 tedy mají velkou rezervu. 128bitový klíč souboru v age je podle NIST také v pořádku, CNSA 2.0 ale chce AES-256.

„Ulož teď, dešifruj později“: EO 14412 tuto hrozbu jmenuje přímo, „adversaries collecting United States information now, and decrypting it later“ [5] (ověřeno). Soubor `.nzo` je pro takový útok ideální cíl: statický ciphertext, který může ležet zkopírovaný desítky let. Klasický KEM sám (X25519, P-384) proto nestačí ani dnes a post-kvantová část musí být v hybridu od první verze. U podpisů je situace jiná: budoucí kvantový útočník by mohl padělat část ECDSA, složený podpis ale vyžaduje i platnou část ML-DSA. Podpis navíc chrání v okamžiku ověření; soubor ověřený dnes budoucí padělek neohrozí.

Režimy s passphrase a s klíčem souboru jsou čistě symetrické a Shorův algoritmus se jich netýká. Grover dává při hledání passphrase nanejvýš kvadratické zrychlení a každé vyhodnocení Argon2id s 2 GiB paměti by v kvantovém obvodu bylo velmi drahé (úvaha).

Bezpečnost ML-KEM je předpoklad o mřížkových problémech. Hybrid s P-384 drží důvěrnost proti klasickému útočníkovi i v případě, že by se ML-KEM ukázal jako slabý. Proti kvantovému útočníkovi v takovém případě nepomůže nic z dnešní nabídky kromě symetrických režimů.

### 2.3 Kde se útočí doopravdy

| Útok | Proč funguje | Co dělá návrh | Co zůstává |
|---|---|---|---|
| Slabá passphrase | Offline hádání nemá žádný limit, server tu není | Passphrase jen vygenerovaná (10 a více slov), Argon2id 2 GiB, vlastní passphrase jen s výslovným přepínačem a varováním | Passphrase zvolená člověkem zůstává slabé místo |
| Napadený počítač odesílatele nebo příjemce | Keylogger, malware, synchronizace do cloudu a zálohy vidí plaintext i klíče | Nic v kryptografii; doporučení plného šifrování disku a čistého stroje | Vysoké, mimo model (T7 v návrhu) |
| Podvržený veřejný klíč při výměně | Kdo vymění soubor identity na USB nebo v e-mailu, čte všechno, co se pak pošle | Povinné porovnání otisku jiným kanálem, uložené kontakty | Uživatel, který otisk nezkontroluje |
| Ukradený soukromý klíč příjemce | Notebook a slabá passphrase identity otevřou všechny dřívější soubory | Argon2id, rotace identit, jednorázové klíče, volitelné druhé tajemství | Bez jednorázových klíčů žádná dopředná bezpečnost |
| Metadata | Název souboru, velikost, časy na souborovém systému, kdo nesl USB | Náhodný výchozí název, PADMÉ, minimum viditelných polí | Časy souborového systému a fyzický přenos |
| Postranní kanály | Časování a spotřeba při operacích se soukromým klíčem | Implementace ze standardní knihovny Go, žádné operace se soukromým klíčem v JavaScriptu | Žádná záruka proti fyzickému přístupu k běžícímu stroji |
| Chyby implementace a parseru | Příjemce otevírá soubor, který mu může podstrčit kdokoli | Specifikace, vektory, fuzzing, pevné délky, limity před alokací | Chyby budou; posouzení nezávislou stranou |
| Zbytky plaintextu na disku | Na SSD nefungují techniky mazání jednotlivých souborů [47] | Soukromý adresář, výstup na stdout, rada šifrovat disk | Plaintext, který jednou ležel na SSD |
| Nátlak na příjemce | Příjemce může být donucen soubor otevřít | Nic (úvaha) | Plně |

I zralé nástroje mají chyby mimo samotnou kryptografii: age v1.2.1 opravoval chybu GHSA-32gq-x56h-299c, kdy název pluginu mohl vést ke spuštění libovolné binárky [9] (ověřeno z poznámek k vydání).

### 2.4 Povolené a zakázané formulace

Smí se tvrdit (po implementaci a testech):

- „Obsah šifruje AES-256-GCM. Klíč souboru chrání hybrid ML-KEM-1024 a P-384, podpis odesílatele kombinuje ML-DSA-87 a ECDSA P-384. Post-kvantové a symetrické parametry odpovídají seznamu CNSA 2.0.“
- „Kdo nemá soukromý klíč příjemce, klíč souboru nebo passphrase, soubor nepřečte, pokud nejsou prolomené zároveň ML-KEM-1024 i P-384, nebo AES-256.“ Jde o předpoklad, dokázat ho nelze.
- „Soubor prozradí, že jde o soubor Nylzo, jaký režim používá, počet příjemců a přibližnou velikost (pásmo PADMÉ). Text, název a typ přílohy, odesílatele ani příjemce bez klíče neukazuje.“
- „Podpis říká, který klíč soubor vytvořil. Kdo za klíčem stojí, víte jen tehdy, když jste otisk ověřili.“

Nesmí se tvrdit: „ani NSA nerozluští“, „vojenská úroveň“, „kvantově odolné“ bez dalšího, „splňuje CNSA 2.0“, „schváleno NSA“, „auditováno“ (dokud audit není), „jednorázové“, „samo se zničí“, „vyprší“, „nevratně smazáno“, „anonymní“, „nedetekovatelné“.

---

## 3. Co offline režim nedá

Soubor je statický ciphertext. Kdo má soubor a klíč, otevře ho kdykoli a kolikrát chce, a soubor jde libovolně kopírovat. Žádnou vlastnost, kterou dnes vynucuje server, proto nejde přenést jako záruku.

| Vlastnost | Serverový režim v1 | Offline `.nzo` | Poctivá náhrada |
|---|---|---|---|
| Jedno otevření | Server atomicky vydá obálku jen tolikrát, kolikrát je nastaveno (Z4) | Neexistuje | Jednorázový klíč příjemce, který se po otevření smaže (níže). Platí jen proti tomu, kdo soubor získá později, ne proti příjemci, který si klíč nebo plaintext zkopíroval |
| Expirace | Server po uplynutí platnosti ciphertext nevydá a smaže | Neexistuje | Žádná. Datum „platí do“ uvnitř souboru by kontroloval jen klient a upravený klient ho ignoruje. Časový zámek přes veřejný beacon (třeba drand) potřebuje síť a třetí stranu, je mimo zadání (úvaha). Doporučení: nenabízet |
| Zrušení odesílatelem | Klíč M, server přestane vydávat | Po předání souboru nejde nic odvolat | Žádná. Odesílatel může jen požádat příjemce, aby soubor a jednorázový klíč smazal |
| Smazání příjemcem (burn) | Server smaže ciphertext | Příjemce smaže svou kopii a jednorázový klíč | Kopie jinde (USB, zálohy) zůstávají |
| Oznámení o otevření a stav | Čas prvního otevření, počet pokusů | Žádný kanál, žádný signál | Žádná. Kdo zkopíroval USB, to nikdo nepozná |
| Limit pokusů o passphrase | Server, 100 pokusů za život zprávy | Neomezené offline hádání | Žádná; proto jen vygenerovaná passphrase a drahé KDF |
| Ochrana proti záměně obsahu | Chybí; OD26 navrhuje podpisový klíč odesílatele, nerozhodnuto | Podpis odesílatele v souboru (kapitola 5) | V souboru vyřešeno, pokud příjemce zná klíč odesílatele |

Co offline režim dává navíc: žádný provozovatel ani hosting nevidí metadata, IP adresy ani časy; žádný síťový útočník ani TLS edge; žádný servírovaný webový klient, který by šlo podvrhnout (Z2 v návrhu), pokud obě strany používají CLI z podepsaného vydání.

Jednorázové klíče příjemce podrobněji: příjemce vygeneruje identitu určenou pro jednu zprávu (`nylzo keygen --one-time`), podepíše její veřejnou část svou dlouhodobou identitou a předá ji odesílateli. Odesílatel ji použije jednou. Příjemce po úspěšném otevření soukromý klíč smaže (`nylzo open --destroy-identity`). Od té chvíle soubor nikdo nerozšifruje, ani kdyby později získal příjemcův počítač i passphrase k dlouhodobé identitě. Výhrady: smazání souboru s klíčem na SSD nemusí data fyzicky odstranit [47], takže útočník se zbytkem zašifrovaného klíče a passphrase by ho obnovit mohl; příjemce si mohl klíč nebo plaintext zkopírovat; odesílatel, který jednorázový klíč použije dvakrát, dopřednou bezpečnost pro obě zprávy ztratí. Formát se kvůli tomu nemění, jde jen o pracovní postup v CLI.

---

## 4. Předání klíče

### 4.1 (a) Veřejný klíč příjemce

Příjemce jednou spustí `nylzo keygen`. Vznikne identita se dvěma páry klíčů: KEM pro šifrování (MLKEM1024-P384) a podpisový pár (ML-DSA-87 a ECDSA P-384), aby příjemce mohl sám posílat podepsané soubory. Veřejná část má 4354 bajtů (1665 + 2592 + 97, velikosti ověřené během běhu v Go 1.27.3 [17]) a předává se jako soubor `.nzid`. Do jednoho QR kódu se nevejde (kapacitu QR jsem neověřoval, běžně se uvádí jednotky kilobajtů), takže QR slouží jen pro otisk.

Otisk: SHA-512 nad doménovým řetězcem a veřejnou částí, zkrácený na 32 bajtů. Pro čtení nahlas stačí prvních 128 bitů jako 10 slov seznamu EFF, pro QR nebo porovnání na obrazovce celých 256 bitů v hex. Druhý vzor otisku by útočník musel najít i s platným párem klíčů, což je při 128 bitech mimo dosah (úvaha).

Výhody: žádné tajemství se nepřenáší s každou zprávou, jedna výměna stačí na libovolný počet souborů, post-kvantový hybrid, podpis odesílatele. Nevýhody: počáteční výměna s ověřením otisku; dlouhodobý soukromý klíč je cíl pro „ulož teď, dešifruj později“ (únik klíče otevře všechny dřívější soubory); správa klíčů na straně příjemce (kapitola 8).

HPKE v režimu Base odesílatele neověřuje: RFC 9180 §9.8 říká „in the Base mode, there is no authentication of the remote party at all“ [27] (ověřeno). Kdo zná veřejný klíč příjemce, může mu poslat soubor tvářící se jako od kohokoli. Proto je v tomto režimu podpis povinný (kapitola 5.4).

### 4.2 (b) Sdílená passphrase

Offline hádání je neomezené, server nic nepočítá. Síla tedy stojí na entropii passphrase a ceně KDF.

| Slov ze seznamu EFF (7776 slov) | Entropie |
|---|---|
| 6 (doporučení EFF pro běžné použití) | zhruba 77,5 bitu |
| 8 | zhruba 103 bitů |
| 10 | zhruba 129 bitů |
| 20 | zhruba 258 bitů |

Seznam má 7776 slov, takže jedno slovo nese log2(7776) ≈ 12,9 bitu; EFF uvádí, že šest slov dává zhruba 2^77 možností a pro většinu použití šest slov doporučuje [40] (ověřeno). Entropii passphrase zvolené člověkem odhadnout nejde, proto ji návrh bere jako slabé místo.

Argon2id podle RFC 9106, první doporučená volba (t=1, p=4, m=2 GiB, sůl 128 bitů, výstup 256 bitů) [28] (ověřeno), přidá každému pokusu konstantní cenu a nízkou entropii nevyrovná. Pro cíl „nejméně 128 bitů“, srovnatelný s post-kvantovou úrovní AES-256, je potřeba 10 slov.

Passphrase musí jít jiným kanálem než soubor: nahlas, na papíře, ne na stejném USB. Ověření odesílatele: kdo passphrase zná, může soubor vytvořit, a příjemce nerozliší odesílatele od jiného držitele passphrase. Specifikace age proto vyžaduje, aby stanza se scrypt byla v souboru jediná, a zdůvodňuje to očekáváním ověření, které šifrování heslem s sebou nese [8]; `.nzo` přebírá stejné pravidlo.

Doporučení: CLI passphrase generuje (výchozí 10 slov, minimum 8). Vlastní passphrase jen s přepínačem `--own-passphrase` a varováním; pod 12 znaků ji odmítne jako protokol v1.

### 4.3 (c) Náhodný 256bitový klíč souboru druhým kanálem

Odesílatel vygeneruje 32 náhodných bajtů a předá je jinou cestou než soubor: na papíře, QR kódem z obrazovky telefonu, na druhém USB, nebo jako 20 slov EFF do telefonu (20 slov nese 258 bitů, pro kontrolu přepisu návrh přidává dvě kontrolní slova z SHA-256). Je to obdoba tajemství S ve fragmentu odkazu v1.

Výhody: 256 bitů bez KDF, žádná počáteční výměna, v prohlížeči jen operace, které má Web Crypto nativně (HKDF, AES-GCM, HMAC). Nevýhody: tajemství na každou zprávu, riziko chyby při přepisu, a pokud klíč cestuje se souborem, ochrana je nulová. Ověření odesílatele stejné jako u passphrase, tedy žádné nad rámec znalosti klíče; podpis lze přidat.

### 4.4 (d) Kombinace: klíč příjemce a druhé tajemství

Soubor otevře jen ten, kdo má soukromý klíč příjemce a zároveň passphrase nebo klíč souboru. Pomůže, když unikne soukromý klíč (ukradený notebook se slabou passphrase identity), nebo když by padly obě části hybridního KEM.

HPKE na to má režim PSK, ten se ale pro passphrase použít nesmí: RFC 9180 §5.1.2 požaduje „The PSK MUST have at least 32 bytes of entropy“ a §9.5 říká, že mechanismus PSK „is not suitable for use with a low-entropy password“ [27] (ověřeno). Dokumentace crypto/hpke režim PSK ani Auth nezmiňuje [12]. Návrh proto používá vlastní kombinátor ve stylu protokolu v1 (tam `S || P`): z kontextu HPKE se exportuje 32bajtové tajemství (`Sender.Export`, ověřeno [12] i během běhu [17]) a klíč pro zabalení klíče souboru je HKDF ze spojení tohoto tajemství a klíče z passphrase nebo klíče souboru. Je to neobvyklá kombinace a patří do seznamu k odbornému posouzení.

### 4.5 Srovnání a doporučení

| Kritérium | (a) klíč příjemce | (b) passphrase | (c) klíč souboru | (d) kombinace |
|---|---|---|---|---|
| Síla bez lidského faktoru | hybrid ML-KEM-1024 a P-384, AES-256 | entropie passphrase plus Argon2id | 256 bitů | obojí |
| Tajemství na každou zprávu | ne | ano | ano | ano |
| Počáteční výměna | ano, s ověřením otisku | ne | ne | ano |
| Ověření odesílatele | podpis (povinný) | jen znalost passphrase | jen znalost klíče | podpis |
| Dopředná bezpečnost | jen s jednorázovými klíči | ano, pokud passphrase zapomenou oba | ano, pokud klíč zničí oba | jako (a) |
| Prohlížeč bez nové knihovny | ne | jen se scrypt | ano | ne |

Doporučení: výchozí režim (a) s povinným podpisem a otiskem ověřeným při výměně. Režim (c) pro jednorázový přenos bez přípravy. Režim (b) jen s vygenerovanou passphrase. Režim (d) jako volba `--also-passphrase` nebo `--also-key` pro nejcitlivější soubory.

---

## 5. Post-kvantová kryptografie a ověření odesílatele

### 5.1 Co má Go dnes

Ověřeno v dokumentaci [12] [13] [14] [15] [16] a během běhu programu v Go 1.27.3 na tomto stroji [17]:

- Go 1.26 (únor 2026) přidalo crypto/hpke, implementaci RFC 9180 včetně post-kvantových hybridních KEM [13]. Go 1.27 (srpen 2026) přidalo crypto/mldsa (FIPS 204) a MLKEM1024 v TLS [14].
- KEM v crypto/hpke: `DHKEM(curve)`, `MLKEM768()`, `MLKEM768P256()`, `MLKEM768X25519()` (dokumentace: „a.k.a. X-Wing“), `MLKEM1024()`, `MLKEM1024P384()`. KDF: HKDF-SHA256/384/512, SHAKE128, SHAKE256. AEAD: AES-128-GCM, AES-256-GCM, ChaCha20-Poly1305, ExportOnly [12].
- crypto/mldsa: `MLDSA44()`, `MLDSA65()`, `MLDSA87()`, kontextový řetězec do 255 bajtů, deterministické podepisování volitelně, soukromý klíč jako 32bajtové semínko [15].
- crypto/mlkem: ML-KEM-1024 má zapouzdřovací klíč i ciphertext 1568 bajtů, semínko 64 bajtů [16].
- Standardní knihovna nemá Ed448, X448 ani SLH-DSA (seznam balíčků crypto/ na pkg.go.dev/std [30]).

Velikosti změřené během běhu [17]:

| KEM | Veřejný klíč | Soukromý klíč | Zapouzdřený klíč (enc) | Zabalený 32B klíč |
|---|---|---|---|---|
| MLKEM1024-P384 | 1665 B | 32 B (semínko) | 1665 B | 48 B |
| MLKEM768-X25519 (X-Wing) | 1216 B | 32 B | 1120 B | 48 B |
| ML-KEM-1024 samotný | 1568 B | 64 B | 1568 B | 48 B |

Chování při cizím klíči: `NewRecipient` s nesprávným soukromým klíčem chybu nevrátí (ML-KEM používá implicitní odmítnutí), selže až otevření zabaleného klíče. Dešifrování proto zkouší každou stanzu a za úspěch bere až platný tag AEAD [17].

### 5.2 Stav standardů

| Dokument | Stav k 23. 10. 2026 | Co definuje |
|---|---|---|
| RFC 9180 HPKE | RFC, 2022 | Rámec HPKE, režimy Base, PSK, Auth, AuthPSK |
| draft-ietf-hpke-pq-05 | pracovní draft skupiny hpke, 6. 7. 2026, „I-D Exists“, bez čísla RFC [18] | ML-KEM-512/768/1024 (0x0040 až 0x0042), MLKEM768-P256 (0x0050), MLKEM1024-P384 (0x0051), MLKEM768-X25519 (0x647a) |
| draft-connolly-cfrg-xwing-kem-11 | individuální draft, Independent Submission, 23. 9. 2026, není RFC ani dokument CFRG [19] | X-Wing (ML-KEM-768 a X25519) |
| draft-irtf-cfrg-concrete-hybrid-kems-04 | dokument CFRG, 6. 7. 2026, Informational, „Revised I-D Needed“ [20] | MLKEM768-P256, MLKEM768-X25519 (shodný s X-Wing), MLKEM1024-P384; KDF SHA3-256, PRG SHAKE-256; rámec z draft-irtf-cfrg-hybrid-kems-12 [21] |
| RFC 10024 (dříve draft-ietf-tls-ecdhe-mlkem) | RFC, srpen 2026, Proposed Standard [22] | TLS skupiny X25519MLKEM768, SecP256r1MLKEM768, SecP384r1MLKEM1024 (1665 B klientský podíl) |
| draft-ietf-lamps-pq-composite-kem-23 | předán IESG, ve frontě RFC Editoru, 14. 10. 2026 [23] | mimo jiné id-MLKEM1024-ECDH-P384-SHA3-256 a id-MLKEM1024-X448-SHA3-256 |
| RFC 9980 (OpenPGP PQC) | RFC, červen 2026, Proposed Standard [25] | povinně ML-KEM-768+X25519 a ML-DSA-65+Ed25519; doporučeně ML-KEM-1024+X448 a ML-DSA-87+Ed448; volitelně SLH-DSA |
| draft-ietf-lamps-pq-composite-sigs-19 | předán IESG, revize 21. 4. 2026, bez čísla RFC [24] | mimo jiné id-MLDSA87-ECDSA-P384-SHA512, id-MLDSA87-Ed448-SHAKE256, id-MLDSA65-Ed25519-SHA512 |

Hybridy s ML-KEM-1024 tedy ve standardech existují: v TLS (RFC 10024, s P-384), v OpenPGP (RFC 9980, s X448), v připravovaném RFC pro kompozitní KEM v X.509 a CMS a v draftu pro HPKE (MLKEM1024-P384). X-Wing je nejrozšířenější hybrid (age, TLS v prohlížečích), formálně ale zůstává individuálním draftem; jeho konstrukci jako MLKEM768-X25519 přebírají draft-ietf-hpke-pq a dokument CFRG.

### 5.3 Volba KEM

| Varianta | Pro | Proti |
|---|---|---|
| MLKEM1024-P384 (doporučeno) | Post-kvantová část v úrovni CNSA 2.0; klasická jistota P-384; v Go stdlib; stejná dvojice jako SecP384r1MLKEM1024 v RFC 10024 | Kombinátor SHA3-256 není v seznamu CNSA; draft HPKE není RFC; o 449 B větší veřejný klíč než X-Wing (pro soubor bezvýznamné) |
| MLKEM768-X25519 (X-Wing) | Nejvíc nasazený hybrid, shodný s age, menší | ML-KEM-768 CNSA 2.0 nepřipouští |
| ML-KEM-1024 samotný | Doslova CNSA 2.0, nejjednodušší | Bez klasické pojistky, kdyby se v ML-KEM našla slabina |

Doporučení: MLKEM1024-P384. Se stejnou sadou by měl počítat i budoucí serverový režim na klíč příjemce (OD19, dnes plánovaný s X-Wing), aby jedna identita fungovala pro obě cesty doručení.

### 5.4 Ověření odesílatele

Proč offline záleží: HPKE Base neověřuje odesílatele [27]. Kdokoli s veřejným klíčem příjemce může na USB podstrčit soubor s podvrženou zprávou nebo škodlivou přílohou. Při více příjemcích zná klíč souboru každý z nich a mohl by obsah pro ostatní vyměnit. Naivní „podepsat a zašifrovat“ navíc trpí přeposíláním: Davis 2001 popisuje, jak Bob zprávu, kterou mu Alice podepsala a zašifrovala, zašifruje znovu pro Charlieho a ten pak věří, že mu psala Alice [44] (ověřeno). Oprava je vázat vnitřní podpis na vnější vrstvu.

Návrh:

- Podpis leží uvnitř šifrovaného obsahu, takže kdo nemá klíč, nevidí ani identitu odesílatele.
- Podepisovaná zpráva obsahuje hash celé hlavičky (včetně zapouzdřených klíčů pro konkrétní příjemce), hash obsahu a seznam otisků příjemců. Přeposlání jinému příjemci se projeví nesouhlasem hlavičky i seznamu.
- Složený podpis podle draft-ietf-lamps-pq-composite-sigs, varianta ML-DSA-87 a ECDSA P-384 se SHA-512: zpráva `M' := Prefix || Label || len(ctx) || ctx || PH(M)`, kde Prefix je ASCII „CompositeAlgorithmSignatures2025“ [24] (ověřeno). Obě části musí projít. Varianty s Ed448 Go stdlib nemá [30]; ML-DSA-65 s Ed25519 je kratší, ale ML-DSA-65 není v CNSA 2.0.
- Velikost: veřejný klíč 2592 + 97 B, podpis 4627 B plus zhruba 100 B ECDSA [15] [17].

Popiratelnost: podpis je přenosný důkaz, příjemce může třetí straně ukázat, že zprávu podepsal klíč odesílatele. HPKE Auth, který by dal popiratelné ověření, potřebuje KEM s AuthEncap, tedy DHKEM [27], a s post-kvantovým KEM nefunguje; crypto/hpke ho navíc nenabízí [12]. Kdo popiratelnost potřebuje víc než ověření, může v režimech (b) a (c) poslat soubor bez podpisu a v režimu (a) jen s výslovným `--unsigned` a varováním u příjemce. Rozhodnutí OD-J4.

LMS a XMSS z CNSA 2.0 jsou stavové podpisy pro firmware; pro uživatele, který podepisuje na více počítačích a ze zálohy, se nehodí, protože opakované použití stavu podpis rozbije (úvaha).

---

## 6. Existující formáty a nástroje

| Kritérium | age v1.3.2 | OpenPGP (RFC 9580 + RFC 9980) | HPKE + vlastní kontejner (návrh) | Picocrypt 1.49 |
|---|---|---|---|---|
| Zralost a údržba | Vydání v1.3.2 29. 8. 2026, PQ od v1.3.0 27. 12. 2025 [9]; specifikace C2SP a vektory CCTV [8] | RFC 9580 (2024) [26], PQC v RFC 9980 (červen 2026) [25]; Go knihovna ProtonMail/go-crypto v1.5.0 (23. 9. 2026) implementuje ML-KEM, ML-DSA a SLH-DSA [32] | Nový formát; HPKE je RFC, crypto/hpke v Go od února 2026 | Archivováno, repozitář jen ke čtení od září 2025 [33] |
| Audit | Formální audit jsem nenašel (vyhledávání) [11]; nález GHSA-32gq-x56h-299c opraven v1.2.1 [9] | Neověřováno | Žádný; nutné nezávislé posouzení | README odkazuje na audit Radically Open Security (zprávu jsem neotevřel) [33] |
| Post-kvantové | mlkem768x25519 (X-Wing) přes HPKE [8] | ano, RFC 9980 | ano, MLKEM1024-P384 | Jen symetrické, Shor se ho netýká |
| Ověření odesílatele | žádné (specifikace ho nepopisuje) [8] | ano, podpisy | ano, složený podpis | ne (jen heslo) |
| Klíč souboru a šifra | 16 B klíč souboru, ChaCha20-Poly1305, bloky 64 KiB, HKDF a HMAC SHA-256 [8] | podle algoritmů v klíči | 32 B, AES-256-GCM, bloky 64 KiB, SHA-512 | XChaCha20, Argon2id; „paranoid“ režim kaskáda se Serpent a HMAC-SHA3 [33] |
| Viditelná metadata | řádek `age-encryption.org/v1`, typy stanz (u scrypt i pracovní faktor), počet příjemců, délka (bez paddingu) [8] [9] | identifikátor klíče příjemce v PKESK, lze vynechat [26] | magic a verze, typy stanz, počet příjemců, pásmo PADMÉ | hlavička s verzí (neověřeno podrobně) |
| Závislosti v Go | filippo.io/age, filippo.io/hpke v0.4.0, filippo.io/nistec v0.0.4 (ostatní už v projektu) [10] | ProtonMail/go-crypto a cloudflare/circl [32] | žádné nové | není knihovna |
| Interoperabilita | age, rage v0.12.1 (Rust) [34] a další | GnuPG, Sequoia a další (neověřováno) | jen Nylzo, dokud někdo neimplementuje zveřejněnou specifikaci | Picocrypt |
| Splňuje měřítko CNSA 2.0 | ne (ML-KEM-768, ChaCha20, 128bitový klíč souboru) | jen s ML-KEM-1024+X448 a ML-DSA-87+Ed448; Ed448 ani X448 v Go stdlib nejsou | ano pro PQ a symetrické části | ne |

Věcné poznámky:

- age je dobře navržený a malý formát a `.nzo` z něj přebírá strukturu: stanzy bez identifikátorů klíče, MAC hlavičky klíčem odvozeným z klíče souboru, payload s náhodnou solí, bloky po 64 KiB s čítačem a příznakem posledního bloku, pravidlo o jediné stanze s heslem, ASCII armour podle RFC 7468 [8]. Specifikace age upozorňuje, že identifikátor klíče ve stanze by připravil ciphertext o anonymitu a nespojitelnost [8].
- Formát age dovoluje vlastní typy stanz, takže by šlo do age přidat stanzu s MLKEM1024-P384. Klíč souboru (16 B) a šifra payloadu (ChaCha20-Poly1305) jsou ale ve specifikaci pevné, takže měřítko CNSA by ani tak nesplnil.
- OpenPGP má podpisy i PQC, ale formát je řádově složitější, nese identifikátor klíče příjemce (lze ho vynechat [26]) a v Go by přinesl dvě velké závislosti. Pro jednoúčelový souborový formát se nevyplatí (úvaha).
- Picocrypt je archivovaný a je to aplikace, ne knihovna. Za zmínku stojí volitelné Reed-Solomon kódování proti poškození média [33]; `.nzo` ho v první verzi nemá.
- Rozpoznatelná hlavička: age soubor prozradí řádek verze a typy příjemců, nástroj age-inspect je vypíše bez dešifrování [9]. `.nzo` prozradí magic a verzi. Soubor nerozlišitelný od náhodných dat by vyžadoval zvláštní kódování ciphertextu ML-KEM a bodu P-384, protože obojí má rozpoznatelnou strukturu (úvaha), a přípona `.nzo` by ho stejně prozradila. Doporučení: viditelný magic s verzí (OD-J10).
- Zda jde ciphertext ML-KEM spojit s veřejným klíčem příjemce: pro Kyber a z něj odvozené hybridní PKE dokazuje anonymitu v post-kvantovém modelu Maram a Xagawa (PKC 2023) [43]. Zda výsledek platí beze změny pro ML-KEM podle FIPS 203 a pro hybrid MLKEM1024-P384, jsem neověřil.

---

## 7. Navržený formát `.nzo` v1

Formát je samostatný dokument (`spec/nzo.md`) s vlastní verzí, nezávislou na protokolu v1. Jedna verze má jednu pevnou sadu algoritmů. Všechna celá čísla jsou big-endian. Řetězce pro odvození klíčů jsou ASCII bez koncové nuly.

### 7.1 Struktura

```
soubor       = magic (8 B) || u32be(header_len) || header || header_mac (32 B) || payload
magic        = 0x89 'N' 'Z' 'O' 0x0D 0x0A 0x1A 0x0A          po vzoru PNG; odhalí převod konců řádků
header       = version (1 B = 0x01) || file_nonce (32 B) || n (1 B, 1 až 16) || stanza × n

stanza 0x01  příjemce:              type || enc (1665 B) || wrapped (48 B)
stanza 0x02  passphrase:            type || kdf_profile (1 B) || salt (32 B) || wrapped (48 B)
stanza 0x03  klíč souboru:          type || salt (32 B) || wrapped (48 B)
stanza 0x04  příjemce a tajemství:  type || enc (1665 B) || secret_kind (1 B: 0x02 passphrase, 0x03 klíč)
                                    || kdf_profile (1 B, 0 u klíče) || salt (32 B) || wrapped (48 B)
```

Klíče:

```
file_key     = 32 B z CSPRNG, nový pro každý soubor
sada HPKE    = KEM MLKEM1024-P384, KDF HKDF-SHA512, AEAD AES-256-GCM, režim Base
info_r       = "nylzo/nzo/v1/recipient" || file_nonce

0x01:  enc || wrapped = HPKE.Seal(pk_příjemce, info_r, file_key)
0x02:  P        = Argon2id(NFC(passphrase) UTF-8, salt, profil, 32)
       k_wrap   = HKDF-SHA512(IKM = P, salt = file_nonce, info = "nylzo/nzo/v1/passphrase", 32)
       wrapped  = AES-256-GCM(k_wrap, iv = 0^12, file_key, aad = magic || version)
0x03:  k_wrap   = HKDF-SHA512(IKM = K_s, salt = salt || file_nonce, info = "nylzo/nzo/v1/key", 32)
       wrapped  = AES-256-GCM(k_wrap, iv = 0^12, file_key, aad = magic || version)
0x04:  E        = HPKE kontext k pk_příjemce s info_r, Export("nylzo/nzo/v1/combined", 32)
       X        = P (Argon2id) nebo K_s
       k_wrap   = HKDF-SHA512(IKM = E || X, salt = salt || file_nonce, info = "nylzo/nzo/v1/recipient+secret", 32)
       wrapped  = AES-256-GCM(k_wrap, iv = 0^12, file_key, aad = magic || version)

k_mac        = HKDF-SHA512(IKM = file_key, salt = file_nonce, info = "nylzo/nzo/v1/header", 32)
header_mac   = HMAC-SHA512(k_mac, magic || u32be(header_len) || header)[0:32]
k_payload    = HKDF-SHA512(IKM = file_key, salt = file_nonce, info = "nylzo/nzo/v1/payload" || header_mac, 32)
```

Bloky s čítačem a příznakem posledního bloku jsou konstrukce STREAM z práce o online autentizovaném šifrování [42], kterou používá i age [8]. Příznak posledního bloku brání tichému zkrácení souboru.

Nulový IV u zabalení je v pořádku, protože každý `k_wrap` šifruje právě jednu zprávu (nová sůl a nový `file_nonce` na soubor); stejný postup používá age s ChaCha20-Poly1305 [8].

Profily KDF jsou pevné a parametry v souboru nejsou: profil 1 je Argon2id t=1, p=4, m=2 GiB, profil 2 je Argon2id t=3, p=4, m=64 MiB (obě volby z RFC 9106 §4 [28]). Útočník tak nemůže podstrčit soubor, jehož otevření by chtělo desítky gigabajtů paměti. Specifikace age jen doporučuje, aby si identity horní mez pracovního faktoru nastavily samy [8].

Payload:

```
vnitřní   = u32be(len_meta) || meta || file_bytes || u32be(len_sig) || sig_block || padding (nuly)
bloky     = vnitřní rozdělený na bloky po 65536 B, poslední může být kratší, ale ne prázdný
blok_i    = AES-256-GCM(k_payload, iv = u88be(i) || last, blok)    last = 0x01 u posledního, jinak 0x00
payload   = blok_0 || blok_1 || ... || blok_last
```

Podpis (jen když `len_sig > 0`):

```
sig_block = pk_mldsa87 (2592 B) || pk_ecdsa_p384 (97 B) || m (1 B, počet příjemců) || fp × m (32 B)
            || composite_sig (ML-DSA-87 4627 B || ECDSA P-384 podle kódování v draftu)
M         = SHA-512(magic || u32be(header_len) || header || header_mac)
            || SHA-512(u32be(len_meta) || meta || file_bytes)
            || m || fp × m
podpis    = složený ML-DSA-87 + ECDSA-P384-SHA512 nad M, ctx = "nylzo/nzo/v1"
```

### 7.2 Pravidla

- Stanzy 0x02 a 0x03 smí být v souboru jen samotné. Stanzy 0x01 a 0x04 se nemíchají s 0x02 a 0x03. Počet stanz 1 až 16 (age v1.3.2 odmítá víc než 1024 příjemců a hlavičky nad 2 MiB [9]; `.nzo` volí nižší strop).
- Stanzy nenesou identifikátor klíče. Příjemce zkouší každou stanzu svým klíčem a úspěch pozná až podle tagu AEAD a podle MAC hlavičky.
- Klíč souboru je vázaný na hlavičku přes HMAC-SHA-512 a `k_payload` závisí na `header_mac`. Kdo klíč souboru nezná, hlavičku nezmění. Příjemce klíč souboru zná a hlavičku i obsah změnit může; to odhalí podpis, který hlavičku i obsah pokrývá. AES-GCM sám klíč nezavazuje (dva klíče mohou otevřít stejný ciphertext); závazek tu dává HMAC hlavičky a podpis nad hashem obsahu. Tuto argumentaci musí potvrdit posouzení (úvaha).
- Čítač bloků má 88 bitů a `k_payload` je nový pro každý soubor, takže se IV pod jedním klíčem nikdy neopakuje.
- Dekodér odmítne soubor bez posledního bloku, data za posledním blokem, nenulový padding, `file_bytes` jiné délky než `meta.file.size` a podpis, který neprojde.

### 7.3 Padding

Délka vnitřního obsahu L se doplní na `padme(max(L, 65536))`. PADMÉ omezuje únik informace z délky na O(log log M) bitů s režií nejvýš 12 % [41] (ověřeno). Funkce (převzít přesně z článku a pokrýt vektory): E = ⌊log2 L⌋, S = ⌊log2 E⌋ + 1, z = E − S, výsledek je L zaokrouhlené nahoru na násobek 2^z. Příklady: 1 000 000 B se doplní na 1 015 808 B, 5 MiB zůstane 5 MiB, 1 GiB se zaokrouhluje na násobky 32 MiB.

Proč ne koše v1 (4 KiB a 64 KiB, D16): u velkých souborů by prozradily velikost na 64 KiB přesně, PADMÉ ji u 1 GiB rozmaže na 32 MiB. Minimum 64 KiB skryje rozdíl mezi textem a malou přílohou, který koše v1 prozrazují (spec §3). Přenos po USB režii snese.

### 7.4 Meta JSON a název souboru

`meta` je přesně objekt ze `spec/protocol.md` §3.1: `{"v":1,"text":"…","file":{"name":"…","mime":"…","size":N}}`, se stejným kanonickým kódováním, se stejnými pravidly pro dekodér (neznámé klíče ignorovat, hloubka nejvýš 32) a se stejnými omezeními názvu. Jádro `internal/core` ho tedy použije znovu; jen limit `file.size` musí být parametr (v1 má 5 MiB). Název přílohy CLI při zápisu očistí stejnou funkcí jako `read` (`internal/cli/filename.go`). Odesílatel, příjemci a čas vytvoření v `meta` nejsou: odesílatel je v `sig_block`, čas vytvoření formát neukládá vůbec.

### 7.5 Co zůstane vidět

Viditelné: magic a verze, náhodný `file_nonce`, typy a počet stanz, profil KDF u passphrase, délka souboru v pásmu PADMÉ. K tomu všechno, co ukládá souborový systém: název souboru a časy vytvoření a změny (FAT32 i exFAT časy ukládají, oprávnění ani ACL nemají [46]).

Neviditelné bez klíče: text, název, typ a velikost přílohy uvnitř pásma, zda jde o text nebo soubor (do 64 KiB), odesílatel, seznam příjemců.

Velikost pro jednoho příjemce: magic, délka, hlavička a MAC 1792 B; podpis zhruba 7,4 kB; obsah doplněný nejméně na 64 KiB; režie bloků 16 B na 64 KiB.

### 7.6 Limity a pořadí kontrol

- `header_len` nejvýš 64 KiB, `len_meta` nejvýš 104 448 B (v1), text nejvýš 100 KiB (v1), `len_sig` nejvýš 16 KiB, `file.size` ve specifikaci nejvýš 2^40 B. CLI má výchozí strop 16 GiB, který jde zvýšit přepínačem. FAT32 má strop souboru 4 GiB [46], CLI proto před zápisem na takový svazek varuje.
- Pořadí při otevření: (1) magic, verze, `header_len`; (2) rozbor stanz s pevnými délkami; (3) rozbalení klíče souboru; (4) MAC hlavičky v konstantním čase; (5) bloky jeden po druhém s omezenou pamětí; (6) `len_meta` a meta JSON, kontrola `file.size` proti limitu a volnému místu ještě před zápisem; (7) zápis přílohy do dočasného souboru v soukromém adresáři s průběžným hashem; (8) podpis a padding; (9) teprve potom přejmenování na konečné názvy a výpis cest. Jakékoli selhání dočasné soubory smaže a vrátí obecnou chybu.

### 7.7 ASCII armour a QR

Armour po vzoru age podle RFC 7468: `-----BEGIN NYLZO FILE-----`, base64 s paddingem, striktní kanonická podoba [8]. Hodí se pro e-mail nebo vložení do textového pole. Nejmenší soubor má kvůli paddingu přes 64 KiB, v armouru zhruba 90 kB, takže armour pro tisk na papír nedává smysl. Do QR kódu se obsah nevejde už kvůli zapouzdřenému klíči (1665 B) a podpisu (přes 7 kB). QR proto slouží jen pro klíč souboru (32 B) a otisk identity.

---

## 8. Uložení soukromého klíče příjemce

Kde: `os.UserConfigDir()/nylzo/identities/`, tedy `$XDG_CONFIG_HOME` nebo `~/.config` na Linuxu, `~/Library/Application Support` na macOS a `%AppData%` na Windows [31] (ověřeno). Adresář 0700, soubory 0600, zápis s `O_EXCL` a přes dočasný soubor a přejmenování. Na Windows Go práva 0600 nepřenese, CLI tam musí nastavit ACL jen pro uživatele nebo to aspoň zkontrolovat (úvaha, ověřit v implementaci).

Co: semínka všech klíčů identity (MLKEM1024-P384 32 B, ML-DSA-87 32 B, ECDSA P-384 48 B), zašifrovaná stejným kódem jako `.nzo` se stanzou passphrase, s vlastním magic `NZK`. Kontakty (veřejné identity protistran s otiskem a jménem) leží vedle v `contacts/` se stejnými právy. Kontakty nejsou tajné, ale jejich záměna je útok MITM, proto je CLI při použití porovná s otiskem uloženým při přidání.

Čím: Argon2id profil 1 (RFC 9106 první volba, 2 GiB) [28], na slabém stroji profil 2 (64 MiB) s varováním. golang.org/x/crypto/argon2 v0.58.0 má `IDKey(password, salt, time, memory, threads, keyLen)` a v dokumentaci cituje obě volby RFC 9106 [29] (ověřeno). scrypt (už v projektu) je rovnocenná paměťově náročná alternativa; Argon2id doporučuji, protože ho OWASP uvádí jako první volbu (příloha A návrhu), RFC 9106 pro něj dává konkrétní parametry a CLI nepotřebuje auditovanou implementaci v prohlížeči. Dobu odvození s 2 GiB jsem neměřil; změří se v implementaci.

Identita bez passphrase: v první verzi nenabízet.

Klíčenky operačního systému: zalando/go-keyring v0.2.8 (23. 3. 2026) na macOS volá `/usr/bin/security` a heslo předává přes stdin, ne v argumentech [39] (ověřeno ve zdroji). Přidává závislost a položka uložená přes nástroj `security` může být čitelná pro jiné procesy téhož uživatele, které stejný nástroj spustí (úvaha, neověřeno). V první verzi nedoporučuji.

Hardwarové klíče později: age-plugin-yubikey v0.5.1 používá PIV s klíčem ECDSA P-256, tedy jen klasickou kryptografii [36]; age-plugin-fido2-hmac v0.5.0 je podle README experimentální, potřebuje C knihovnu libfido2 a při dešifrování přenáší identitu do paměti počítače [37]; age-plugin-se pro Secure Enclave existuje (v0.2.1) [38], algoritmus jsem neověřoval. Hardwarový klíč s ML-KEM jsem nenašel (nehledal jsem systematicky). Nejrozumnější pozdější varianta je FIDO2 hmac-secret jako druhý faktor k passphrase při odemčení souboru identity: je symetrický, takže ho kvantový počítač neohrozí. Cenou je cgo, které dnešní build (`CGO_ENABLED=0`) vylučuje.

---

## 9. CLI

Pravidla z D55 platí beze změny: žádné tajemství v argumentu ani přepínači, žádné poziční argumenty, tajemství ze stdin, ze souboru nebo z terminálu bez echa, výstup do nového adresáře 0700 se soubory 0600 a `O_EXCL`, návratové kódy 0, 1 a 2. Tvar ze zadání `nylzo open x.nzo` by poziční argument potřeboval, proto cesta jde přes `--in`.

| Příkaz | Co dělá |
|---|---|
| `nylzo keygen --name NAME [--public-out FILE]` | Vytvoří identitu, passphrase dvakrát z terminálu nebo z `--passphrase-file`, zapíše veřejnou část `NAME.nzid`, vypíše otisk (10 slov a hex) |
| `nylzo keygen --one-time --signed-by NAME` | Jednorázová identita podepsaná dlouhodobou identitou |
| `nylzo key show --name NAME` | Otisk a veřejná část vlastní identity |
| `nylzo contact add --from-file alice.nzid --name alice` | Uloží veřejnou identitu protistrany; uživatel musí opsat slova otisku, která mu protistrana přečetla, CLI je porovná |
| `nylzo contact list`, `nylzo contact remove --name alice` | Správa kontaktů |
| `nylzo seal --to alice [--to bob] [--from-file F] --out x.nzo` | Režim (a), text ze stdin nebo příloha ze souboru, podpis výchozí identitou |
| `nylzo seal --key --key-out FILE ...` nebo `--print-key` | Režim (c), klíč do nového souboru 0600 nebo na stdout jako slova |
| `nylzo seal --passphrase [--own-passphrase] ...` | Režim (b), passphrase vygeneruje a vypíše, vlastní jen s přepínačem |
| `nylzo seal ... --also-passphrase` nebo `--also-key` | Režim (d) |
| `nylzo seal ... --armor`, `--unsigned` | Armour; soubor bez podpisu (u režimu (a) s varováním) |
| `nylzo open --in x.nzo [--identity NAME] [--key-file F \| --ask-key \| --passphrase-file F \| --ask-passphrase] [--out DIR \| --stdout] [--destroy-identity]` | Otevře soubor, ověří podpis proti kontaktům, zapíše výstup a vypíše cesty |
| `nylzo inspect --in x.nzo` | Ukáže, co je vidět bez klíče (verze, režim, počet stanz, velikost), jako age-inspect |

Chování:

- `seal` zapisuje výstup jen jako nový soubor. Výchozí název je náhodný (`nzo-` a 8 znaků base32), aby neprozradil obsah. Když `--key-out` míří na stejný svazek jako `--out`, CLI varuje, že klíč a soubor cestují spolu.
- `open` bez známého podpisu odmítne pokračovat, dokud uživatel nepřidá `--allow-unknown-sender`; neznámý odesílatel s platným podpisem se vypíše otiskem. Když `--out` leží na stejném svazku jako vstup (typicky USB), CLI varuje, že plaintext skončí na přenosném médiu.
- `open --stdout` vypíše jen text; přílohu vždy zapíše do souboru.
- Chybová hláška řekne, ve kterém kroku otevření selhalo (klíč, hlavička, obsah, podpis), a nikdy neobsahuje tajemství. Offline nikdo neposílá dotazy jako na server, takže podrobnější hláška nedává útočníkovi orákulum (úvaha).

Příklad:

```
příjemce$ nylzo keygen --name bob --public-out bob.nzid
odesílatel$ nylzo contact add --from-file bob.nzid --name bob        # opíše 10 slov, která Bob přečte
odesílatel$ nylzo seal --to bob --from-file smlouva.pdf --out /Volumes/USB/nzo-k4q7m2xa.nzo < poznamka.txt
příjemce$ nylzo open --in /Volumes/USB/nzo-k4q7m2xa.nzo
/var/folders/.../nylzo-3fj2/message.txt
/var/folders/.../nylzo-3fj2/smlouva.pdf
```

---

## 10. Prohlížeč a `.nzo`

Technicky by šlo, aby `nylzo ui` spustil vložený webový klient na 127.0.0.1 s náhodným portem. Podle specifikace Secure Contexts je loopback „Potentially Trustworthy“ a Web Crypto tam funguje. U `file://` nechává specifikace rozhodnutí na prohlížeči: neprůhledný origin je „Not Trustworthy“ a schéma `file` prohlížeč za důvěryhodné brát smí, ale nemusí [49] (ověřeno). Navíc tam nefunguje nonce v CSP, který dnes doplňuje server. Na `file://` tedy nespoléhat.

Co by to stálo:

- ML-KEM-1024 ani ML-DSA-87 ve Web Crypto nejsou, v Chromu jen experiment (příloha A a review-crypto návrhu). Zbývá @noble/post-quantum 0.7.1 (27. 8. 2026): podle README knihovna zatím nemá nezávislý audit, autor ji v dubnu 2026 (verze 0.6.1) posoudil sám a implementace netvrdí běh v konstantním čase [35] (ověřeno). Operace se soukromým klíčem (dekapsulace) v takové knihovně jsou slabší proti postranním kanálům než v Go.
- Argon2id v prohlížeči nemá auditovanou implementaci (návrh, P5). Režim passphrase by v prohlížeči potřeboval scrypt, což jde proti OD-J6.
- Soukromý klíč identity by v JavaScriptu existoval jako bajty. Neextrahovatelný CryptoKey, jaký dnes klient používá pro K a M, knihovna nabídnout nemůže.
- Třetí implementace jádra (Go, TS pro v1, TS pro `.nzo`), kterou drží pohromadě jen vektory.
- Velké soubory: číst jde proudově, zapsat výstup o velikosti gigabajtů ale vyžaduje API, které není ve všech prohlížečích (neověřeno).

Doporučení: v první verzi ne. Později jen režim (c), který potřebuje HKDF, AES-GCM a HMAC z Web Crypto a pro ověření podpisu veřejnou operaci ML-DSA (u ověřování postranní kanály nevadí).

---

## 11. Hrozby přenosu na USB

| Hrozba | Popis | Zmírnění | Zbytek |
|---|---|---|---|
| Škodlivý USB disk | Firmware USB disku jde přeprogramovat, aby se tvářil jako jiné zařízení, třeba klávesnice; ukázáno na Black Hat 2014 [48] | Nový disk z důvěryhodného zdroje; `.nzo` nic nespouští | Napadený počítač tím `.nzo` neochrání |
| Autorun | Od Windows 7 AutoPlay nepodporuje AutoRun u vyměnitelných médií, která nejsou optická, tedy ani u USB disků; MSRC ale upozorňuje, že některé disky se tváří jako CD/DVD [45] | Nylzo nic ze svazku nespouští; přílohy neotevírat přímo z USB | Disk, který se tváří jako CD |
| Název a metadata souboru | Název volí uživatel; FAT32 a exFAT ukládají časy a nemají oprávnění [46] | Náhodný výchozí název; žádný čas uvnitř souboru | Časy souborového systému |
| Zbytky po smazání | Na flash pamětech nefungují techniky mazání jednotlivých souborů navržené pro disky [47] | Na USB jen ciphertext, nikdy plaintext ani klíč | Smazaný `.nzo` lze obnovit, bez klíče je ale k ničemu |
| Velikost | Délka souboru | PADMÉ, minimum 64 KiB [41] | Pásmo velikosti |
| Změna po cestě | Poškození nebo úmyslná změna | MAC hlavičky, AEAD bloků, podpis | Poškozený soubor nejde otevřít (dostupnost); Reed-Solomon jako pozdější volba |
| Podvržený soubor | Cizí soubor zašifrovaný na veřejný klíč příjemce | Podpis proti kontaktům, neznámý odesílatel jen s přepínačem | Uživatel, který varování odklikne |
| Útok na parser | Příjemce otevírá soubor od kohokoli | Pevné délky, stropy před alokací, pevné profily KDF, žádná dekomprese, meta JSON s limitem hloubky, proudové zpracování; fuzzing nativní v Go od 1.18 [50] pro hlavičku, stanzy, bloky, meta, armour a podpis; testy s každým bitem hlavičky převráceným | Chyby v neotestovaných cestách |
| Plaintext na disku | Výstup zůstane na disku, ve swapu, zálohách, indexu a synchronizovaných složkách | Soukromý adresář v dočasném adresáři, `--stdout` pro text, varování při výstupu na USB, rada šifrovat disk (FileVault, BitLocker, LUKS) | Kopie, které vytvoří OS a aplikace |
| Škodlivá příloha | Odesílatel (i ověřený) pošle škodlivý soubor (T5) | Očištěný název, žádné automatické otevření, varování u spustitelných přípon jako D35 | Uživatel přílohu otevře |
| Ztracený disk | Někdo najde USB | V režimu (a) potřebuje soukromý klíč, v (b) passphrase, v (c) klíč, který na disku není | Slabá passphrase |

---

## 12. Rozhodnutí pro vlastníka

| Označení | Rozhodnutí | Možnosti | Doporučení |
|---|---|---|---|
| OD-J1 | Formát | (A) vlastní kontejner nad crypto/hpke; (B) age v1.3 jako vnější vrstva a vnitřní obsah Nylzo s podpisem a paddingem; (C) OpenPGP RFC 9580 a 9980 | A: splní parametry CNSA 2.0, bez nové závislosti, podpis a padding v jednom návrhu. Cena: vlastní specifikace a posouzení. B, pokud má přednost interoperabilita a zralost před měřítkem CNSA |
| OD-J2 | Sada KEM | MLKEM1024-P384; X-Wing; čistý ML-KEM-1024 | MLKEM1024-P384 |
| OD-J3 | Výchozí předání klíče | (a) klíč příjemce; (c) klíč souboru; (b) passphrase | (a), k tomu (c) a (b) jako volby |
| OD-J4 | Podpis odesílatele | povinný v (a), volitelný v (b) a (c), `--unsigned` jen výslovně; vždy povinný; nikdy (popiratelnost) | První možnost |
| OD-J5 | Podpisová sada | ML-DSA-87 + ECDSA P-384 (kompozitní draft); ML-DSA-65 + Ed25519; ML-DSA-87 samotný | ML-DSA-87 + ECDSA P-384 |
| OD-J6 | KDF pro passphrase a klíč v klidu | Argon2id 2 GiB s profilem 64 MiB; scrypt N=2^20 (kvůli prohlížeči) | Argon2id |
| OD-J7 | Padding | PADMÉ s minimem 64 KiB; koše v1; mocniny dvou; pevné třídy | PADMÉ s minimem 64 KiB |
| OD-J8 | Jednorázové klíče příjemce | v první verzi jednotlivě (`--one-time`); později i balíčky klíčů; vůbec | Jednotlivě v první verzi, balíčky později |
| OD-J9 | Prohlížeč | ne v první verzi; později jen režim (c); plná podpora | Ne v první verzi, později (c) |
| OD-J10 | Viditelná hlavička | magic a verze; soubor bez rozpoznatelné struktury | Magic a verze |
| OD-J11 | Kombinovaný režim (d) | volitelně; vůbec | Volitelně, s posouzením kombinátoru |
| OD-J12 | Expirace a „spálit po přečtení“ v souboru | nenabízet; nabízet jako nezávazný údaj | Nenabízet |
| OD-J13 | Vztah k OD19 a OD26 | jedna identita a jedna sada KEM pro `.nzo` i budoucí serverový režim na klíč příjemce (OD19 přejde z X-Wing na sadu z OD-J2); oddělené | Jedna identita a jedna sada. OD26 (podpisový klíč na zprávu v odkazu) je jiná věc: ML-DSA-87 má veřejný klíč 2592 B a do fragmentu se nevejde, takže OD26 by v post-kvantové podobě potřeboval klíč v obálce a jeho hash ve fragmentu; kód pro složený podpis mohou sdílet |

---

## 13. Odhad pracnosti a závislosti

| Část | Člověkodny |
|---|---|
| Specifikace `spec/nzo.md`, vektory generované z Go | 3 až 4 |
| Jádro: hlavička, stanzy, bloky, padding, podpis, armour | 5 až 7 |
| Identity, kontakty, uložení klíčů, jednorázové identity | 3 až 4 |
| CLI příkazy a práce s terminálem a soubory | 3 až 4 |
| Testy: vektory, fuzzing, mutační kontrola bezpečnostních pravidel, e2e mezi dvěma konfiguracemi, převrácené bity, limity, přeposlání podle Davise | 4 až 5 |
| Dokumentace, model hrozeb, stránka transparentnosti | 2 |
| Celkem | 20 až 26 |

Bez externího kryptografického posouzení, jehož cenu ani termín neznám. Prohlížeč jen pro režim (c) přidá 5 až 8 dní. Plná podpora v prohlížeči přidá 15 až 20 dní a JS knihovnu bez nezávislého auditu.

Nové závislosti: žádný nový Go modul. Argon2id je balíček modulu golang.org/x/crypto v0.58.0, který projekt už má. Do binárky přibude seznam slov EFF (7776 slov); jeho licenci jsem neověřoval. Volitelně později: knihovna pro vykreslení QR v terminálu (nevybírána), zalando/go-keyring pro klíčenky, libfido2 přes cgo pro FIDO2, @noble/post-quantum pro prohlížeč.

---

## 14. Co se nepodařilo ověřit

- Primární dokumenty CNSA 2.0 (advisory a FAQ v2.2 na media.defense.gov): 403 přes WebFetch, curl i headless prohlížeč. Seznam algoritmů beru z draftu autorů z NSA [4], termíny 2027 a 2030 z nsa.gov [2]. Tabulka termínů podle kategorií, postoj k hybridům a formulace „pro všechny stupně utajení“ neověřeny.
- CNSSP 15 na cnss.gov: stránka se v headless prohlížeči nenačetla.
- Formální audit age: vyhledávání žádný nenašlo; nepřítomnost výsledku není důkaz, že audit neexistuje.
- Auditní zprávu Picocryptu jsem neotevřel.
- Platnost výsledku o anonymitě Kyberu [43] pro ML-KEM podle FIPS 203 a pro hybrid MLKEM1024-P384.
- Formát soukromého klíče hybridu v crypto/hpke: během běhu má 32 B, popis v draftu jsem nečetl.
- Doba odvození Argon2id 2 GiB na cílových strojích (neměřeno).
- Čitelnost položek klíčenky macOS uložených přes `/usr/bin/security` pro jiné procesy (úvaha).
- Kapacita QR kódu, licence seznamu slov EFF, chování `file://` a zápisu velkých souborů v jednotlivých prohlížečích.
- Příslušnost P-384 a ECDSA P-384 k CNSA 1.0 (z paměti, v této rešerši neotevřeno).

---

## 15. Zdroje

Všechny zdroje otevřeny 23. 10. 2026, pokud není uvedeno jinak.

| # | Zdroj | URL | Co dokládá | Stav |
|---|---|---|---|---|
| 1 | NSA, Post-Quantum Cybersecurity Resources | https://www.nsa.gov/Cybersecurity/Post-Quantum-Cybersecurity-Resources/ | CNSSP 15 ze 4. 3. 2025, odkaz na FAQ CNSA 2.0 v2.2 | ověřeno (headless prohlížeč gstack) |
| 2 | NSA, Post-Quantum Cryptography Resource Hub | https://www.nsa.gov/Cybersecurity/Post-Quantum-Cryptography-Resource-Hub/ | nové NSS do 2027, vyřazení starších do 2030 | ověřeno (headless prohlížeč gstack) |
| 3 | NSA, CNSA 2.0 FAQ v2.2 a advisory | https://media.defense.gov/2022/Sep/07/2003071836/-1/-1/0/CSI_CNSA_2.0__FAQ_v2.2.PDF | termíny, hybridy | neověřeno (403) |
| 4 | draft-becker-cnsa2-tls-profile-05 | https://www.ietf.org/archive/id/draft-becker-cnsa2-tls-profile-05.html | seznam algoritmů CNSA 2.0 | ověřeno |
| 5 | Executive Order 14412 | https://www.whitehouse.gov/wp-content/uploads/2026/06/eo-14412.pdf | „collecting now, decrypting later“, termíny 2030 a 2031 | ověřeno (text vytažen z PDF) |
| 6 | NIST PQC FAQ | https://csrc.nist.gov/projects/post-quantum-cryptography/faqs | Grover a AES | ověřeno |
| 7 | NIST IR 8547 | https://csrc.nist.gov/pubs/ir/8547/ipd | stále návrh z 12. 11. 2024 | ověřeno |
| 8 | C2SP, specifikace age | https://github.com/C2SP/C2SP/blob/main/age.md | 16B klíč souboru, stanzy, scrypt jen samotný, X-Wing příjemce, payload, armour, žádné identifikátory klíčů | ověřeno |
| 9 | age, vydání (GitHub API) | https://api.github.com/repos/FiloSottile/age/releases | v1.3.0 27. 12. 2025 PQ, v1.2.1 GHSA-32gq-x56h-299c, v1.3.2 29. 8. 2026 limity hlavičky, age-inspect | ověřeno |
| 10 | age, go.mod | https://raw.githubusercontent.com/FiloSottile/age/main/go.mod | závislosti filippo.io/hpke v0.4.0, filippo.io/nistec v0.0.4 | ověřeno |
| 11 | Vyhledávání auditu age | (WebSearch „age encryption filippo audit“) | formální audit nenalezen | nenalezeno |
| 12 | Go, crypto/hpke | https://pkg.go.dev/crypto/hpke | KEM, KDF, AEAD, Export, žádná zmínka o PSK a Auth | ověřeno |
| 13 | Go 1.26 release notes | https://go.dev/doc/go1.26 | crypto/hpke s hybridy | ověřeno |
| 14 | Go 1.27 release notes | https://go.dev/doc/go1.27 | crypto/mldsa, MLKEM1024 v TLS | ověřeno |
| 15 | Go, crypto/mldsa | https://pkg.go.dev/crypto/mldsa | parametry, velikosti, kontext, FIPS modul v1.0.0 bez ML-DSA | ověřeno |
| 16 | Go, crypto/mlkem | https://pkg.go.dev/crypto/mlkem | velikosti ML-KEM-1024 | ověřeno |
| 17 | Vlastní běh v Go 1.27.3 (scratchpad, mimo repozitář) | | velikosti klíčů a enc, Export, chování při cizím klíči, ML-DSA-87, P-384 | ověřeno měřením |
| 18 | draft-ietf-hpke-pq | https://datatracker.ietf.org/doc/draft-ietf-hpke-pq/ | verze 05, KEM a kódy | ověřeno |
| 19 | draft-connolly-cfrg-xwing-kem | https://datatracker.ietf.org/doc/draft-connolly-cfrg-xwing-kem/ | verze 11, individuální draft | ověřeno |
| 20 | draft-irtf-cfrg-concrete-hybrid-kems | https://datatracker.ietf.org/doc/draft-irtf-cfrg-concrete-hybrid-kems/ | verze 04, tři hybridy, SHA3-256 a SHAKE-256 | ověřeno |
| 21 | draft-irtf-cfrg-hybrid-kems | https://datatracker.ietf.org/doc/html/draft-irtf-cfrg-hybrid-kems-12 | verze 12 z července 2026 | částečně (jen výsledek vyhledávání) |
| 22 | RFC 10024 | https://datatracker.ietf.org/doc/draft-ietf-tls-ecdhe-mlkem/ | TLS hybridy včetně SecP384r1MLKEM1024 | ověřeno |
| 23 | draft-ietf-lamps-pq-composite-kem | https://datatracker.ietf.org/doc/draft-ietf-lamps-pq-composite-kem/ | verze 23, ML-KEM-1024 kompozity | ověřeno |
| 24 | draft-ietf-lamps-pq-composite-sigs | https://datatracker.ietf.org/doc/draft-ietf-lamps-pq-composite-sigs/ | verze 19, varianty, konstrukce M' | ověřeno |
| 25 | RFC 9980 | https://datatracker.ietf.org/doc/draft-ietf-openpgp-pqc/ | OpenPGP PQC | ověřeno |
| 26 | RFC 9580 | https://www.rfc-editor.org/info/rfc9580 a https://www.rfc-editor.org/rfc/rfc9580.html | OpenPGP, PKESK a skrytí příjemce | ověřeno |
| 27 | RFC 9180 | https://www.rfc-editor.org/rfc/rfc9180.html | entropie PSK, Base bez ověření odesílatele, Auth jen s AuthEncap | ověřeno |
| 28 | RFC 9106 §4 | https://www.rfc-editor.org/rfc/rfc9106.html#section-4 | doporučené parametry Argon2id | ověřeno |
| 29 | golang.org/x/crypto/argon2 | https://pkg.go.dev/golang.org/x/crypto/argon2 | `IDKey`, v0.58.0 | ověřeno |
| 30 | Go, seznam balíčků std | https://pkg.go.dev/std | žádné Ed448, X448, SLH-DSA | ověřeno (curl) |
| 31 | Go, os.UserConfigDir | https://pkg.go.dev/os#UserConfigDir | cesty podle OS | ověřeno |
| 32 | ProtonMail/go-crypto, vydání | https://api.github.com/repos/ProtonMail/go-crypto/releases | v1.5.0 23. 9. 2026 s ML-KEM, ML-DSA, SLH-DSA, circl | ověřeno |
| 33 | Picocrypt | https://github.com/Picocrypt/Picocrypt | archivace, algoritmy, odkaz na audit | ověřeno; audit částečně |
| 34 | rage, vydání | https://api.github.com/repos/str4d/rage/releases/latest | v0.12.1 14. 7. 2026 | ověřeno |
| 35 | @noble/post-quantum | https://github.com/paulmillr/noble-post-quantum | 0.7.1, bez nezávislého auditu, bez konstantního času | ověřeno |
| 36 | age-plugin-yubikey | https://github.com/str4d/age-plugin-yubikey | v0.5.1, PIV ECDSA P-256 | ověřeno |
| 37 | age-plugin-fido2-hmac | https://github.com/olastor/age-plugin-fido2-hmac | v0.5.0, experimentální, libfido2 | ověřeno |
| 38 | age-plugin-se | https://github.com/remko/age-plugin-se | v0.2.1 existuje | částečně |
| 39 | zalando/go-keyring, keyring_darwin.go | https://github.com/zalando/go-keyring | v0.2.8, `security -i` přes stdin | ověřeno |
| 40 | EFF, Dice-Generated Passphrases | https://www.eff.org/dice | šest slov ≈ 2^77, doporučení šesti slov | ověřeno |
| 41 | Nikitin a kol., PURBs a PADMÉ, PoPETs 2019(4) | https://www.petsymposium.org/popets/2019/popets-2019-0056.php | O(log log M), režie nejvýš 12 % | ověřeno |
| 42 | Hoang, Reyhanitabar, Rogaway, Vizár, Online AE | https://eprint.iacr.org/2015/189 | OAE2, základ konstrukce STREAM | částečně (název STREAM na stránce není) |
| 43 | Maram, Xagawa, Post-Quantum Anonymity of Kyber | https://eprint.iacr.org/2022/1696 | anonymita Kyberu a hybridních PKE | ověřeno |
| 44 | Davis, Defective Sign & Encrypt, USENIX 2001 | https://www.usenix.org/legacy/event/usenix01/full_papers/davis/davis_html/ | přeposílání podepsané zprávy | ověřeno |
| 45 | MSRC, AutoRun changes in Windows 7 (28. 4. 2009) | https://www.microsoft.com/en-us/msrc/blog/2009/04/autorun-changes-in-windows-7 | AutoRun pro USB vypnutý, disky tvářící se jako CD | ověřeno |
| 46 | Microsoft Learn, File System Functionality Comparison | https://learn.microsoft.com/en-us/windows/win32/fileio/filesystem-functionality-comparison | FAT32 4 GiB, časy, žádné ACL | ověřeno |
| 47 | Wei a kol., Reliably Erasing Data From Flash-Based SSDs, FAST 2011 | https://www.usenix.org/events/fast11/tech/full_papers/Wei.pdf | mazání jednotlivých souborů na SSD nefunguje | ověřeno (abstrakt z PDF) |
| 48 | Nohl, Lell, BadUSB, Black Hat USA 2014 | https://infocondb.org/con/black-hat/black-hat-usa-2014/badusb-on-accessories-that-turn-evil | přeprogramování USB disků | částečně (katalog konference) |
| 49 | W3C Secure Contexts (Editor's Draft, 10. 11. 2023) | https://w3c.github.io/webappsec-secure-contexts/ | loopback a `file` | ověřeno |
| 50 | Go, fuzzing | https://go.dev/doc/security/fuzz/ | fuzzing ve standardním toolchainu od Go 1.18 | ověřeno |

Interní zdroje: `docs/designs/nylzo-architecture-research.md` (Z1 až Z10, T1 až T14, OD19, OD26, D16, D35, D55, příloha A), `spec/protocol.md` §2 až §4 a §11, `docs/research/A-crypto.md`, `docs/research/review-crypto.md`, `internal/cli/filename.go`.
