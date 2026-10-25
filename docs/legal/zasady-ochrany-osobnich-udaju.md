# Zásady ochrany osobních údajů

Návrh k posouzení advokátem. Platné od: [DOPLNIT: datum zveřejnění]. Verze: 0.1.

Tyto zásady popisují, jak veřejná instance Nylzo na adrese nylzo.com (dále jen „služba“) zachází s osobními údaji. Nylzo je open-source software pod licencí MIT a kdokoli ho může provozovat na vlastním serveru. Na jiné instance se tyto zásady nevztahují; odpovídá za ně ten, kdo je provozuje.

## 1. Správce

Správcem osobních údajů je Ondřej Misák, [DOPLNIT: adresa pro doručování; IČO, pokud službu provozuje jako podnikatel] (dále jen „provozovatel“). Kontakt pro otázky k osobním údajům: [DOPLNIT: e-mail]. Pověřence pro ochranu osobních údajů provozovatel nejmenoval.

[⚠️ K POSOUZENÍ ADVOKÁTEM: Zda fyzická osoba, která službu provozuje neplaceně a hlavně pro sebe, nespadá pod výjimku pro čistě osobní činnost (čl. 2 odst. 2 písm. c) GDPR). Protože je služba otevřená veřejnosti, návrh počítá s tím, že výjimka neplatí.]

## 2. Shrnutí

- Služba nemá účty a nechce po vás jméno, e-mail ani telefon.
- Zprávu zašifruje váš prohlížeč dřív, než ji odešle. Klíč je v části odkazu za znakem #, kterou prohlížeč serveru neposílá, takže provozovatel obsah zprávy ani název přílohy nepřečte.
- Server neukládá IP adresy na disk a nezapisuje je do logu. Používá je jen v paměti pro ochranu před zahlcením a po restartu je zapomene.
- Stránka nepoužívá cookies, analytiku, reklamu ani skripty třetích stran.
- Zpráva se smaže po posledním otevření, zrušení, smazání příjemcem nebo krátce po konci platnosti (nejvýše 7 dní). Krátký záznam o tom, jak skončila, zůstane 7 dní.

## 3. Jaké údaje služba zpracovává

### 3.1 Obsah zpráv

Text a přílohu zprávy zašifruje váš prohlížeč algoritmem AES-256-GCM. Server dostane a uloží jen zašifrovaná data. Klíč k nim je v odkazu za znakem #; prohlížeč tuto část odkazu serveru neposílá a provozovatel ji nemá. Obsah zprávy ani název přílohy proto provozovatel přečíst nemůže a nevidí ani to, zda zpráva obsahuje osobní údaje.

Pokud do zprávy vložíte osobní údaje jiných lidí, odpovídáte za to, že je smíte předat.

[⚠️ K POSOUZENÍ ADVOKÁTEM: Zda je zašifrovaný obsah, který provozovatel nemůže dešifrovat, z pohledu provozovatele osobním údajem (rozsudek SDEU C-413/23 P, EDPS proti SRB, 4. 9. 2025), a jaká je role provozovatele vůči obsahu (správce, nebo zpracovatel pro odesílatele). Podklady jsou v docs/research/G-eu-legal.md, část 1.]

### 3.2 Údaje o zprávě

Aby mohl zprávu vydat a smazat, ukládá server ke každé zprávě:

- náhodný identifikátor z odkazu,
- zda zprávu chrání passphrase, verzi formátu a nastavení odesílatele (počet otevření, platnost),
- stav zprávy (čeká, otevřená, nebo jak skončila),
- kolik otevření zbývá, kolikrát někdo zadal špatnou passphrase a po chybných pokusech, kdy je možný další pokus,
- velikost zarovnanou nahoru na celé bloky (zprávu jen s textem na 4 KiB, zprávu s přílohou na 64 KiB),
- čas vytvoření, konce platnosti a prvního otevření, zaokrouhlené na minutu,
- otisky veřejných klíčů (HMAC s tajemstvím serveru), podle kterých server ověřuje podepsané požadavky,
- během otevírání záznam o probíhajícím stažení (náhodný identifikátor, jeho otisk, čas konce a počet stažení).

Žádný z těchto údajů neobsahuje IP adresu, jméno ani jiný údaj o tom, kdo zprávu poslal nebo otevřel.

### 3.3 IP adresa

Každý požadavek na server nese IP adresu vašeho připojení. Server ji neukládá do databáze ani do souboru a nezapisuje ji do logu. V paměti z ní odvodí krátké otisky samotné adresy a jejího širšího rozsahu (HMAC s náhodným klíčem, který vznikne při každém spuštění serveru a nikam se neukládá) a podle nich počítá požadavky za sekundu, hodinu a den. Otisky zůstávají v paměti, dokud se tabulka, která je drží, nezaplní a nezahodí staré záznamy, nejdéle do restartu serveru.

Aby poznal náhledy odkazů z chatovacích aplikací, čte server v paměti také hlavičku User-Agent. Neukládá ji ani nezapisuje do logu.

IP adresu vidí také poskytovatel hostingu a zařízení, které ukončuje šifrované spojení HTTPS (viz část 6).

### 3.4 Provozní záznamy (logy)

Server zapisuje do logu čas zkrácený na minutu, metodu a typ adresy požadavku, výsledek a dobu vyřízení. U vytvoření, otevření a stažení zprávy dobu vyřízení vynechává, protože by prozradila velikost zprávy. Do logu nikdy nezapisuje IP adresu, identifikátor zprávy, klíče, podpisy, obsah ani velikost zprávy.

### 3.5 Hlášení zneužití

Když nahlásíte zprávu jako zneužitou, server uloží datum, identifikátor zprávy a vámi uvedený důvod, ale jen pokud zpráva ještě existuje. Stránka v důvodu před odesláním nahradí textem „[redacted]“ každý znak # i s textem za ním do nejbližší mezery a každou souvislou řadu 22 a více písmen, číslic, pomlček nebo podtržítek, aby se do hlášení nedostal klíč z odkazu; server totéž před uložením udělá znovu. Server nezjišťuje, kdo hlášení poslal, a neukládá k němu IP adresu ani kontakt.

### 3.6 Počítadla bez adres

Pro ochranu služby server ukládá počet hlášení u každé zprávy za den a krátkodobě použité jednorázové výzvy a tokeny ochrany proti zahlcení. Pokud provozovatel nastavil denní strop nových zpráv, ukládá i denní počty vytvořených zpráv. Žádné z nich neobsahuje IP adresu.

### 3.7 Data uložená jen ve vašem prohlížeči

Některé údaje ukládá stránka do úložiště vašeho prohlížeče. Na server je neposílá, s výjimkou případů uvedených níže.

| Úložiště | Co obsahuje | Kdy a na jak dlouho |
|---|---|---|
| localStorage | zvolený jazyk | jen pokud jazyk změníte; dokud data prohlížeče nesmažete |
| sessionStorage | během otevírání zprávy její identifikátor a token stažení vydaný serverem; značka, že se nedokončilo odvození klíče z passphrase | do zavření karty |
| IndexedDB | seznam odeslaných zpráv: identifikátor, časy a klíč pro zrušení, který prohlížeč nedovolí vyexportovat | ve výchozím stavu, pokud „Zrušení z tohoto zařízení“ nevypnete; záznam se odstraní při další návštěvě po 7 dnech od konce platnosti zprávy (nebo od jejího skončení, pokud ho stránka Odeslané zprávy zjistila), nebo když ho zapomenete |

Token stažení prohlížeč posílá zpět na server, aby stažení dokončil nebo navázal. Když otevřete stránku Odeslané zprávy, prohlížeč se serveru zeptá na stav každé zapamatované zprávy a pošle k tomu identifikátor, veřejnou část klíče pro zrušení a podpis; totéž pošle při zrušení zprávy. Pokud máte uložený jazyk, úvodní stránka vás přesměruje na českou nebo anglickou adresu, takže server vidí, kterou jste otevřeli.

[⚠️ K POSOUZENÍ ADVOKÁTEM: Zda toto ukládání spadá pod výjimku pro technicky nezbytné ukládání podle § 89 odst. 3 zákona č. 127/2005 Sb. a čl. 5 odst. 3 směrnice 2002/58/ES, takže nevyžaduje souhlas (rozbor v docs/research/G-eu-legal.md, část 7).]

### 3.8 Co služba nezpracovává

Služba nemá uživatelské účty a nezpracovává jméno, e-mail ani telefon. Nepoužívá cookies, analytické nástroje, reklamu ani skripty, písma nebo jiné soubory třetích stran. Údaje neprodává ani nepředává pro marketing.

## 4. Účely a právní základy

| Účel | Údaje | Právní základ |
|---|---|---|
| Uložit zprávu a vydat ji držiteli odkazu | zašifrovaný obsah, údaje o zprávě | plnění smlouvy, tedy těchto podmínek užívání (čl. 6 odst. 1 písm. b) GDPR) |
| Ochrana služby před zahlcením a zneužitím | IP adresa v paměti, počítadla bez adres | oprávněný zájem provozovatele na bezpečném a dostupném provozu (čl. 6 odst. 1 písm. f) GDPR) |
| Provoz a řešení chyb | provozní logy | oprávněný zájem provozovatele (čl. 6 odst. 1 písm. f) GDPR) |
| Vyřízení hlášení nezákonného obsahu | hlášení zneužití | právní povinnost podle čl. 16 nařízení (EU) 2022/2065 (DSA) (čl. 6 odst. 1 písm. c) GDPR) |
| Funkce stránky ve vašem prohlížeči | data podle části 3.7 | technicky nezbytné ukládání, které jste si vyžádali |

[⚠️ K POSOUZENÍ ADVOKÁTEM: Volba právních základů, zejména zda bezplatné užívání bez registrace zakládá smlouvu (písm. b), nebo zda je vhodnější oprávněný zájem (písm. f), a test proporcionality k oprávněnému zájmu.]

## 5. Jak dlouho údaje uchováváme

| Údaje | Doba uchování |
|---|---|
| Zašifrovaný obsah | do posledního otevření, zrušení, smazání příjemcem, odstranění po hlášení nebo konce platnosti (nejvýše 7 dní); smaže se při dalším požadavku na zprávu nebo při pravidelném úklidu, který ve výchozím nastavení běží každých 5 minut; po posledním otevření, které prohlížeč nepotvrdil, až uplyne lhůta na stažení (nejméně 2 minuty) |
| Údaje o zprávě | do skončení zprávy; poté jen záznam o tom, jak a kdy skončila, kdy byla poprvé otevřena, a otisky jejích klíčů, po dobu 7 dní |
| Záznam o probíhajícím stažení | do potvrzení stažení, nebo do prvního úklidu po konci jeho lhůty (nejméně 2 minuty, u velkých zpráv déle) |
| IP adresa a odvozené otisky v paměti | dokud je tabulka v paměti nezahodí, nejdéle do restartu serveru |
| Provozní logy | [DOPLNIT: podle nastavení hostingu, doporučeno nejvýše 7 dní. Příklad s Docker Compose logy rotuje jen podle velikosti (3 soubory po 10 MB) a časový limit nemá] |
| Hlášení zneužití | nejvýše 7 dní ode dne hlášení |
| Denní počítadla | 24 hodin po konci dne |
| Jednorázové výzvy a tokeny ochrany proti zahlcení | několik minut |
| Data v prohlížeči | podle tabulky v části 3.7 |

Smazané záznamy databáze server přepisuje nulami. Soubory se zašifrovaným obsahem maže; jejich zbytky na disku zůstávají zašifrované a bez klíče je nikdo nepřečte.

## 6. Komu údaje předáváme

- Poskytovatel hostingu: [DOPLNIT: název, sídlo, země, kde jsou servery]. Provozuje fyzický server a jako zpracovatel vidí IP adresy, časy a velikosti požadavků a zašifrovaná data. Obsah zpráv nevidí. [⚠️ K POSOUZENÍ ADVOKÁTEM: smlouva o zpracování osobních údajů s poskytovatelem hostingu podle čl. 28 GDPR.]
- Šifrované spojení HTTPS ukončuje [DOPLNIT: kdo; v doporučeném nasazení přes Docker Compose je to proxy Caddy na stejném serveru, kterou ovládá provozovatel a která nezapisuje přístupy ani podrobnosti chyb]. Kdo spojení ukončuje, vidí IP adresy a požadavky, ne však obsah zpráv.
- Orgány veřejné moci: jen pokud to ukládá zákon a jen údaje, které služba má. Obsah zpráv provozovatel vydat nemůže, protože k němu nemá klíč.

Jiným osobám provozovatel údaje nepředává.

## 7. Předávání mimo EU

Provozovatel nemá v úmyslu předávat osobní údaje mimo Evropský hospodářský prostor. Servery služby jsou v [DOPLNIT: země EU]. [⚠️ K POSOUZENÍ ADVOKÁTEM: doplnit podle zvoleného hostingu; u poskytovatele se sídlem mimo EU posoudit předání a přístup podle práva třetí země (docs/research/G-eu-legal.md, části 2 a 3).]

## 8. Vaše práva

Podle GDPR máte právo na přístup ke svým údajům, jejich opravu, výmaz, omezení zpracování, přenositelnost a právo vznést námitku proti zpracování na základě oprávněného zájmu. Máte také právo podat stížnost u Úřadu pro ochranu osobních údajů, Pplk. Sochora 27, 170 00 Praha 7, www.uoou.cz, nebo u dozorového úřadu ve státě, kde žijete.

Služba nemá účty a neukládá IP adresy, takže provozovatel zpravidla nedokáže určit, které údaje patří vám (čl. 11 GDPR). Svou zprávu můžete zrušit na stránce Odeslané zprávy v prohlížeči, ve kterém jste ji vytvořili (pokud jste nevypnuli „Zrušení z tohoto zařízení“), nebo tokenem pro správu v programu nylzo. Jako příjemce ji můžete smazat. Pokud se na provozovatele obracíte kvůli konkrétní zprávě, uveďte jen její identifikátor (část odkazu před znakem #). Celý odkaz nikomu neposílejte, protože obsahuje klíč.

Na žádosti odpoví provozovatel bez zbytečného odkladu, nejpozději do jednoho měsíce.

[⚠️ K POSOUZENÍ ADVOKÁTEM: znění části o čl. 11 a 12 GDPR pro službu bez účtů.]

## 9. Zabezpečení

- Šifrování probíhá ve vašem prohlížeči nebo v programu nylzo (CLI); klíč server nikdy nedostane.
- Server ukládá místo klíčů jen jejich otisky s tajemstvím, které je mimo databázi.
- Kopie databáze, souborů a logů neprozradí obsah zpráv ani IP adresy; ověřuje to automatický test při každé změně kódu.
- Kód, specifikace protokolu a popis toho, co server ukládá, jsou veřejné. Podrobnosti a limity konkrétní instance ukazuje stránka Transparentnost.

Žádný systém není dokonale bezpečný. Šifrování neochrání zprávu na napadeném zařízení odesílatele nebo příjemce a nezabrání příjemci, aby si obsah uložil.

## 10. Děti

Služba neověřuje věk. Není určena dětem mladším 15 let bez dohledu zákonného zástupce. [⚠️ K POSOUZENÍ ADVOKÁTEM: zda je věková hranice potřebná, když zpracování nestojí na souhlasu (čl. 8 GDPR, § 7 zákona č. 110/2019 Sb.).]

## 11. Automatizované rozhodování

Služba nerozhoduje o lidech automatizovaně ve smyslu čl. 22 GDPR. Její technické ochrany platí pro všechny stejně: limity počtu požadavků z jedné adresy nebo rozsahu adres, čekání po pěti chybných passphrase a zamčení po stovce, strop ukládaných hlášení a odmítnutí, když serveru dojde kapacita nebo místo na disku. Mohou dočasně odmítnout požadavek.

## 12. Změny zásad

Novou verzi zásad provozovatel zveřejní na této stránce s datem účinnosti. O podstatné změně upozorní na úvodní stránce služby.

## 13. Kontakt

Otázky a žádosti k osobním údajům posílejte na [DOPLNIT: e-mail].
