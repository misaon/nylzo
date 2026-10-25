# Zásady ochrany osobních údajů Nylzo

Platné od [DOPLNIT: datum].

## Správce

- Správcem je provozovatel služby nylzo.com, soukromá osoba, která službu provozuje bezplatně a mimo své podnikání. Kontakt: [DOPLNIT: e-mail].
- Veřejnou instanci nenabízíme jako zpracovatele podle čl. 28 GDPR. Organizace, která potřebuje smlouvu o zpracování, si může Nylzo provozovat sama.

## Co zpracováváme

- Obsah zprávy: šifruje ho váš prohlížeč a my ho nepřečteme. Protože nevíme, co obsahuje, bereme ho jako osobní údaj a jen ho uložíme, vydáme a smažeme. O obsahu a příjemcích rozhoduje odesílatel.
- Údaje o zprávě: nastavení odesílatele, stav, počet otevření a chybných passphrase, velikost a časy na minutu. Ukládáme je zapečetěné a přečíst je umíme jen během požadavku toho, kdo má odkaz nebo správcovský klíč. Čitelně zůstává jen otisk identifikátoru a čas, kdy údaje smažeme. Neobsahují jméno ani IP adresu.
- IP adresa: jen v paměti serveru pro limity proti zahlcení, převedená na otisk s náhodným klíčem. Na disk ani do logu se nezapisuje a po restartu zmizí. Hlavičku User-Agent server čte v paměti, aby poznal náhledy odkazů.
- Logy: čas na minutu, druh požadavku a výsledek. Bez IP adresy, identifikátoru zprávy a obsahu.
- Hlášení zneužití: datum, identifikátor zprávy, důvod bez kódů z odkazu, potvrzení dobré víry a e-mail, pokud ho uvedete. Server hlášení zapečetí klíčem provozovatele a sám ho nepřečte.

Úplný a aktuální výčet ukazuje stránka Transparentnost.

## Proč a na jakém základě

| Účel | Právní základ (čl. 6 odst. 1 GDPR) |
|---|---|
| Uložit, vydat a smazat zprávu | písm. b) vůči odesílateli, jinak písm. f) |
| Ochrana proti zahlcení a hádání passphrase | písm. f), oprávněný zájem na dostupnosti a bezpečnosti; údaje jsou jen v paměti a bez profilování |
| Provoz a řešení chyb (logy) | písm. f) |
| Vyřízení hlášení | písm. c) s čl. 16 DSA, pokud se DSA uplatní, jinak písm. f) |
| Vyřízení vašich žádostí | písm. c) |

Proti zpracování podle písm. f) můžete vznést námitku.

## Jak dlouho

- Obsah: do posledního otevření, zrušení nebo smazání, nejpozději krátce po konci platnosti (nejvýše 7 dní).
- Údaje o skončené zprávě: 7 dní, zaokrouhleno nahoru na celou hodinu.
- Otisky IP adres: v paměti, nejdéle do restartu serveru.
- Hlášení včetně e-mailu: 7 dní od dne hlášení.
- Logy: [DOPLNIT, nejvýše 7 dní].

## Ve vašem prohlížeči

- localStorage: zvolený jazyk, jen když ho změníte.
- sessionStorage: token stažení během otevírání zprávy, do zavření karty.
- IndexedDB: seznam odeslaných zpráv s klíčem pro zrušení, jen když zapnete „Zrušení z tohoto zařízení“. Stránka Odeslané zprávy podle něj zjišťuje stav zpráv.
- Je to nezbytné pro funkce, o které žádáte, proto to nevyžaduje souhlas (§ 89 odst. 3 zákona č. 127/2005 Sb.). Cookies ani analytiku nepoužíváme.

## Komu údaje předáváme

- Poskytovateli hostingu [DOPLNIT] jako zpracovateli. HTTPS ukončuje [DOPLNIT].
- Úřadům jen tehdy, když to ukládá zákon. Obsah zpráv vydat nemůžeme, protože k němu nemáme klíč, a údaje o zprávách bez odkazu nepřečteme.
- Údaje zůstávají v EU [DOPLNIT podle hostingu].

## Vaše práva

- Máte právo na přístup, opravu, výmaz, omezení zpracování, přenositelnost a námitku.
- Stížnost můžete podat u Úřadu pro ochranu osobních údajů (Pplk. Sochora 27, 170 00 Praha 7, uoou.gov.cz, posta@uoou.gov.cz) nebo u úřadu ve státě, kde žijete.
- Účty ani IP adresy neukládáme a údaje o zprávě bez odkazu nebo správcovského klíče nepřečteme, takže vás obvykle nepoznáme (čl. 11 GDPR). Údaje o své zprávě vidíte sami na stránce Odeslané zprávy nebo příkazem `nylzo status`. Celý odkaz nikdy neposílejte, obsahuje klíč.
- Odpovíme do jednoho měsíce, v odůvodněných případech nejvýše o dva měsíce později.

## Ostatní

- Věk neověřujeme a žádné zpracování nestojí na souhlasu.
- Automatizované rozhodování podle čl. 22 GDPR neprovádíme.
- Novou verzi zásad zveřejníme s datem účinnosti.
