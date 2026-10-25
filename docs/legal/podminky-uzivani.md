# Podmínky užívání

Návrh k posouzení advokátem. Platné od: [DOPLNIT: datum zveřejnění]. Verze: 0.1.

Tyto podmínky upravují užívání veřejné instance Nylzo na adrese nylzo.com (dále jen „služba“). Software Nylzo je open source pod licencí MIT; jeho užívání, úpravy a šíření upravuje tato licence, ne tyto podmínky. Kdo provozuje vlastní instanci, stanoví pro ni vlastní pravidla.

## 1. Provozovatel

Službu provozuje Ondřej Misák, [DOPLNIT: adresa pro doručování; IČO, pokud službu provozuje jako podnikatel] (dále jen „provozovatel“), jako neplacený soukromý projekt. Provozuje ji především pro vlastní potřebu a dává ji k dispozici i ostatním.

[⚠️ K POSOUZENÍ ADVOKÁTEM: Zda je neplacená služba provozovaná fyzickou osobou „službou informační společnosti“, která se běžně poskytuje za úplatu, a tedy zda se na ni vztahuje nařízení (EU) 2022/2065 (DSA). Návrh počítá s tím, že ano, a plní povinnosti hostingové služby podle čl. 11, 12, 14, 16 a 17 (rozbor v docs/research/G-eu-legal.md, část 4).]

## 2. Přijetí podmínek

Užíváním služby s těmito podmínkami souhlasíte. Pokud s nimi nesouhlasíte, službu neužívejte. Služba je bezplatná a nevyžaduje registraci.

[⚠️ K POSOUZENÍ ADVOKÁTEM: vymahatelnost podmínek přijatých samotným užíváním bez výslovného potvrzení a uzavření smlouvy se spotřebitelem bez registrace.]

## 3. Jak služba funguje

- Zprávu zašifruje váš prohlížeč (nebo program nylzo) dřív, než ji odešle. Klíč je v části odkazu za znakem #, kterou prohlížeč serveru neposílá. Provozovatel obsah zprávy nezná a nemůže ho obnovit ani vydat.
- Zprávu otevře, kdo má odkaz, a to nejvýše tolikrát, kolikrát odesílatel nastavil. Pokud odesílatel nastavil passphrase, je potřeba i ta.
- Po posledním otevření, zrušení odesílatelem, smazání příjemcem nebo po konci platnosti (nejvýše 7 dní) server zprávu smaže. Smazanou zprávu nelze obnovit.
- Služba slouží k jednorázovému předání, ne k uchovávání dat. Nepoužívejte ji jako jediné místo, kde máte uloženou důležitou informaci.
- Aktuální limity instance (velikost přílohy, nejdelší platnost, počet otevření, počet zpráv) ukazuje stránka Transparentnost.

## 4. Pravidla užívání

Službu smíte užívat jen v souladu s právem a s těmito podmínkami. Zejména ji nesmíte užívat k tomu, abyste:

- šířili obsah, který je podle práva České republiky nebo Evropské unie nezákonný, například materiály zobrazující sexuální zneužívání dětí, teroristický obsah, podněcování k násilí nebo nenávisti,
- podváděli jiné lidi, například phishingem, podvodnými výzvami k platbě nebo vydáváním se za někoho jiného,
- šířili malware nebo jiný škodlivý kód,
- porušovali práva jiných, například autorská práva, ochranné známky, obchodní tajemství nebo právo na ochranu osobních údajů a soukromí,
- vyhrožovali jiným lidem, obtěžovali je nebo pronásledovali,
- posílali nevyžádaná hromadná sdělení,
- obcházeli limity a ochranu služby proti zahlcení, zahlcovali ji nebo na ni útočili.

Za obsah zprávy odpovídá ten, kdo ji vytvořil.

## 5. Jak provozovatel postupuje proti nezákonnému obsahu

Provozovatel obsah zpráv nevidí, nekontroluje ho a nemá k tomu technické prostředky. Postupuje na základě oznámení (část 6):

- Každé oznámení, které služba uloží, posoudí člověk. Ukládá nejvýše tři oznámení na zprávu za den, a jen ke zprávám, které ještě existují. Nic se nemaže automaticky.
- Pokud provozovatel dojde k závěru, že zpráva porušuje právo nebo tyto podmínky, zprávu smaže. Příjemce pak 7 dní při otevření odkazu uvidí, že zpráva byla smazána, stejně jako po smazání příjemcem; potom, že neexistuje.
- Automatické nástroje služba používá jen k vlastní ochraně: limity počtu požadavků z jedné adresy nebo rozsahu adres; pokud je nastavený, denní strop nově vytvořených zpráv, od jehož poloviny musí prohlížeč před vytvořením zprávy provést krátký výpočet; čekání po pěti chybných passphrase a zamčení po stovce; a odmítnutí, když serveru dojde kapacita nebo místo na disku. Platí pro všechny stejně a o obsahu nerozhodují.
- Služba neukládá kontakt na odesílatele ani na příjemce, a proto jim provozovatel nemůže rozhodnutí zaslat. Proti rozhodnutí se můžete ohradit na kontaktu v části 7.

[⚠️ K POSOUZENÍ ADVOKÁTEM: jak splnit povinnost odůvodnění podle čl. 17 DSA, když provozovatel nezná elektronický kontakt dotčené osoby, a zda postačuje stav „odstraněno“ zobrazený u odkazu.]

## 6. Oznámení nezákonného obsahu

Zprávu, která podle vás obsahuje nezákonný obsah nebo porušuje tyto podmínky, nahlaste formulářem na stránce Nahlásit zneužití (nylzo.com/report) nebo tlačítkem Nahlásit na stránce zprávy, které je dostupné před otevřením a po otevření, dokud zbývají otevření. Formulář pošle serveru jen identifikátor zprávy, důvod, veřejný klíč odvozený z odkazu, jednorázovou výzvu serveru a podpis. Odkaz ani obsah zprávy stránku neopustí.

Uveďte, proč považujete obsah za nezákonný. Pokud chcete znát výsledek, napište navíc na kontakt v části 7 a uveďte jen identifikátor zprávy. Celý odkaz neposílejte, protože obsahuje klíč.

Provozovatel oznámení vyřizuje bez zbytečného odkladu. Služba přijímá oznámení jen ke zprávám, které ještě existují, uchová nejvýše tři oznámení na zprávu za den a smaže je 7 dní po dni, kdy přišla.

[⚠️ K POSOUZENÍ ADVOKÁTEM: zda formulář bez kontaktu na oznamovatele splňuje čl. 16 DSA (potvrzení přijetí a vyrozumění oznamovatele), co znamená „bez zbytečného odkladu“ a zda je přípustný strop tří oznámení na zprávu a den.]

## 7. Kontaktní místo

Pro uživatele i orgány veřejné moci (čl. 11 a 12 DSA) je kontaktní místo: [DOPLNIT: e-mail]. Komunikovat lze česky a anglicky. Kontakt je uveden také na stránce Transparentnost.

## 8. Bez záruky

Provozovatel službu vytvořil s co největší péčí o bezpečnost, ale poskytuje ji tak, jak je, bez jakékoli záruky. Zejména nezaručuje, že:

- bude služba dostupná nepřetržitě a bez chyb,
- zpráva bude doručena nebo otevřena,
- služba bude odolná proti všem útokům,
- bude služba provozována i v budoucnu.

Službu může provozovatel kdykoli změnit, omezit nebo ukončit, i bez předchozího upozornění.

Šifrování neochrání zprávu na napadeném zařízení odesílatele nebo příjemce. Kdo má odkaz jako první, může zprávu otevřít. Příjemce si obsah může uložit. Odkaz a passphrase posílejte jen lidem, kterým věříte, a každý jinou cestou.

## 9. Odpovědnost

Provozovatel v nejširším rozsahu, který právo dovoluje, neodpovídá za újmu vzniklou užíváním služby nebo tím, že ji užívat nelze, zejména za:

- ztrátu nebo smazání zprávy,
- nedostupnost služby,
- únik obsahu z napadeného zařízení, prozrazením odkazu nebo jednáním příjemce,
- užití služby jinými lidmi v rozporu s těmito podmínkami.

Nic v těchto podmínkách nevylučuje ani neomezuje odpovědnost, kterou právo vyloučit nebo omezit nedovoluje. Jde zejména o odpovědnost za újmu na přirozených právech člověka a za újmu způsobenou úmyslně nebo z hrubé nedbalosti (§ 2898 zákona č. 89/2012 Sb., občanský zákoník) a o práva spotřebitele, kterých se nelze vzdát.

[⚠️ K POSOUZENÍ ADVOKÁTEM: znění omezení odpovědnosti vůči spotřebitelům (§ 1814 a § 2898 občanského zákoníku), případně horní hranice náhrady u bezplatné služby.]

## 10. Duševní vlastnictví

Software Nylzo je dostupný pod licencí MIT na github.com/misaon/nylzo. Licence se týká kódu a souborů v repozitáři. Nedává právo používat název Nylzo tak, aby jiná služba vypadala jako tato služba nebo jako služba, kterou provozovatel podporuje. Obsah zpráv patří těm, kdo je vytvořili. Provozovateli z něj nevznikají žádná práva kromě toho, co je nutné k uložení a vydání zašifrované zprávy.

[⚠️ K POSOUZENÍ ADVOKÁTEM: ochrana názvu a loga Nylzo, které nejsou registrovanou ochrannou známkou a jejichž soubory jsou v repozitáři pod licencí MIT.]

## 11. Omezení přístupu

Provozovatel může smazat zprávu, která porušuje tyto podmínky nebo právo, a dočasně omezit přístup ke službě z rozsahu adres, ze kterého přichází zahlcení nebo útok, a to automatickými limity nebo blokováním v síti. Svou zprávu můžete zrušit v prohlížeči, ve kterém jste ji vytvořili, nebo tokenem pro správu v programu nylzo, a jako příjemce ji můžete smazat.

## 12. Ochrana osobních údajů

Jak služba zachází s osobními údaji, popisují Zásady ochrany osobních údajů na [DOPLNIT: adresa stránky].

## 13. Změny podmínek

Novou verzi podmínek provozovatel zveřejní s datem účinnosti a upozorní na ni na úvodní stránce. Pro zprávy vytvořené před změnou platí podmínky ve znění ke dni jejich vytvoření.

## 14. Rozhodné právo a spory

Tyto podmínky se řídí právem České republiky. Spory rozhodují české soudy. Pokud jste spotřebitel, zůstává vám ochrana, kterou vám poskytuje právo státu, kde máte obvyklé bydliště.

[⚠️ K POSOUZENÍ ADVOKÁTEM: volba práva a soudu vůči spotřebitelům z jiných členských států (čl. 6 nařízení Řím I, nařízení Brusel I bis) a zda se na neplaceného nepodnikajícího provozovatele vztahuje povinnost informovat o mimosoudním řešení spotřebitelských sporů (ČOI).]

## 15. Závěrečná ustanovení

Pokud by některé ustanovení těchto podmínek bylo neplatné, ostatní zůstávají v platnosti. Podmínky jsou vydány česky a anglicky; v případě rozporu má přednost česká verze.

[⚠️ K POSOUZENÍ ADVOKÁTEM: přednost jazykové verze vůči spotřebitelům, kteří česky nerozumí.]
