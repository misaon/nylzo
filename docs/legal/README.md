# Právní dokumenty veřejné instance

Návrhy podmínek užívání a zásad ochrany osobních údajů pro veřejnou instanci Nylzo na nylzo.com, česky a anglicky. Nejsou právní radou a před zveřejněním je musí posoudit advokát. Vycházejí z rešerše [docs/research/G-eu-legal.md](../research/G-eu-legal.md) a z toho, co server skutečně dělá podle [spec/protocol.md](../../spec/protocol.md).

| Dokument | Česky | Anglicky |
|---|---|---|
| Podmínky užívání | [podminky-uzivani.md](podminky-uzivani.md) | [terms-of-use.md](terms-of-use.md) |
| Zásady ochrany osobních údajů | [zasady-ochrany-osobnich-udaju.md](zasady-ochrany-osobnich-udaju.md) | [privacy-policy.md](privacy-policy.md) |

Značky v textech: `[DOPLNIT: …]` a `[TO FILL IN: …]` jsou údaje, které doplní provozovatel; `[⚠️ K POSOUZENÍ ADVOKÁTEM: …]` a `[⚠️ LEGAL REVIEW REQUIRED: …]` jsou otázky pro advokáta.

## Shrnutí

- Provozovatel: Ondřej Misák jako fyzická osoba, neplacený soukromý projekt, hlavně pro vlastní potřebu a otevřený ostatním.
- Bez účtů, cookies, analytiky a služeb třetích stran. Obsah zpráv šifruje prohlížeč a provozovatel ho nepřečte.
- Osobní údaje na serveru: údaje o zprávách bez IP adres, IP adresy jen v paměti pro limity, minimální logy, hlášení zneužití bez kontaktu na oznamovatele.
- Doby uchování: obsah nejvýše 7 dní, záznam o skončené zprávě 7 dní, hlášení nejvýše 7 dní ode dne hlášení, denní počítadla 24 hodin po konci dne, logy podle hostingu.
- Podmínky: zakázané užití podle čl. 14 DSA, postup při oznámení podle čl. 16, kontaktní místo podle čl. 11 a 12, žádné záruky a omezení odpovědnosti v mezích § 2898 občanského zákoníku, české právo.

## Co doplní provozovatel

1. Adresu pro doručování, případně IČO.
2. Kontaktní e-mail (stejný jako `NYLZO_CONTACT` na stránce Transparentnost).
3. Poskytovatele hostingu, jeho sídlo a zemi serverů, a kdo ukončuje HTTPS.
4. Jak dlouho platforma hostingu drží logy serveru (doporučeno nejvýše 7 dní). Příklad s Docker Compose rotuje logy jen podle velikosti (3 soubory po 10 MB), takže časový limit je potřeba nastavit zvlášť.
5. Datum účinnosti a adresy stránek, na kterých budou dokumenty zveřejněné.

## Otázky pro advokáta

1. Vztahuje se na neplacenou službu fyzické osoby nařízení DSA (služba informační společnosti „běžně poskytovaná za úplatu“) a výjimka pro čistě osobní činnost v GDPR?
2. Je služba „hostingová služba“, a ne „online platforma“ (čl. 3 písm. g) bod iii a písm. i) DSA)?
3. Jak splnit čl. 16 a 17 DSA (potvrzení oznámení, vyrozumění, odůvodnění) bez jakéhokoli kontaktu na uživatele a co znamená „bez zbytečného odkladu“.
4. Je zašifrovaný obsah, který provozovatel nemůže dešifrovat, pro něj osobním údajem (C-413/23 P, EDPS proti SRB)? Je provozovatel vůči obsahu správcem, nebo zpracovatelem?
5. Právní základy zpracování (smlouva, nebo oprávněný zájem) a test proporcionality.
6. Ukládání v prohlížeči jako technicky nezbytné ukládání bez souhlasu (§ 89 odst. 3 zákona č. 127/2005 Sb.).
7. Znění vyloučení záruk a omezení odpovědnosti vůči spotřebitelům (§ 1814 a § 2898 občanského zákoníku).
8. Vymahatelnost podmínek přijatých samotným užíváním, volba práva a soudu vůči spotřebitelům z jiných států, informace o mimosoudním řešení sporů.
9. Věková hranice a přednost jazykové verze.
10. Ochrana názvu a loga Nylzo, které nejsou registrovanou ochrannou známkou.

## Další kroky

1. Doplnit údaje a nechat dokumenty posoudit advokátem.
2. Zveřejnit je jako stránky webového klienta s odkazem v patičce a na stránce Transparentnost. Text webu už na podmínky odkazuje („použití v rozporu s podmínkami“) a stránky zatím chybí.
3. Hlášení zneužití kontrolovat aspoň jednou za pár dní. Server na nová hlášení neupozorňuje a po 7 dnech je smaže, takže podmínka „každé hlášení posoudí člověk“ platí, jen když je provozovatel opravdu čte.
4. Při každé změně toho, co server ukládá nebo loguje (spec §13), upravit i zásady ochrany osobních údajů.
