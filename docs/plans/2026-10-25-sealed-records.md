# Plán: zapečetěné záznamy (D101)

Datum: 25. 10. 2026. Větev: `feat/sealed-records`, postavená na PR 34 (`fix/report-and-sent-list`). Zdroj: rešerše L (`docs/research/L-metadata.md`) a OD28.

## Rozhodnutí vlastníka

Vlastník 25. 10. 2026 rozhodl zabezpečit jen aplikaci, a to co nejvíc. Provoz serveru (šifrovaný disk, Tang) ani síť (Tor, OHTTP) neřešíme.

## D101: ukradený server neprozradí metadata zpráv

- Řádek zprávy má místo id lokátor `SHA-256("nylzo/v1/locator" || id)`. Všechny údaje zprávy (politika, stav, časy, počty, mac ověřovače, lease, počet hlášení) jsou v záznamu šifrovaném AES-256-GCM náhodným klíčem záznamu. Ten server ukládá zabalený klíči odvozenými z `pk_R` a `pk_M`. Ty dostane jen při vytvoření a v podepsaném požadavku a neukládá je, takže záznam otevře jen během požadavku držitele odkazu nebo správcovského klíče. Rozbalení nahrazuje porovnání `read_mac` a `manage_mac`.
- Blob má náhodný odkaz a vlastní klíč `blob_key`, uložený zabalený klíčem záznamu. Server ho ukládá jako AES-256-CTR s výplní Padmé, takže hlavička obálky ani přesná velikost nejsou čitelné. Mazání začíná zahozením `blob_wrap`: od té chvíle nejde přečíst ani blob, ani jeho zbytky ve volných blocích disku.
- Úklid záznamy neotevírá. Čitelné jsou jen dva časy úklidu: smazání blobu zaokrouhlené nahoru na čtvrthodinu a smazání řádku na celou hodinu.
- Hlášení zneužití server šifruje HPKE (`crypto/hpke`, MLKEM768-X25519) veřejným klíčem provozovatele a doplní na jednu délku. Soukromý klíč zůstává mimo server; `nylzo report-key` vytvoří pár klíčů, `nylzo reports` hlášení přečte. Bez `NYLZO_REPORT_KEY` server nenastartuje.
- `nylzo takedown` záznam otevřít neumí: smaže blob a řádek nechá do úklidu. Příjemce a odesílatel uvidí `deleted` bez `ended_at`.
- Proces na Linuxu zakáže core dump a čtení své paměti jiným procesem téhož uživatele.
- Schéma 5 zahodí zprávy ze schématu 4, protože je bez klíčů nejde zapečetit. Žádné vydání schéma 4 nepoužilo.

## Co zůstane čitelné v ukradené kopii

Počet zpráv za posledních zhruba 7 dní, zda řádek ještě má blob, dva zaokrouhlené časy úklidu, třída velikosti blobu podle Padmé, počet hlášení za den, globální denní čítače a časy souborů v souborovém systému (`ctime`), které aplikace nastavit nemůže.

## Cena

- Blob se po vypršení smaže nejpozději 15 minut plus interval úklidu po expiraci; řádek žije nejvýš o hodinu déle než 7 dní po konci zprávy.
- Bloby zaberou na disku nejvýš o 12 % víc.
- Server drží klíč záznamu v paměti do konce posledního lease, aby blob smazal hned po něm.
- Provozovatel musí před startem vytvořit klíč pro hlášení.

## Nezávislá kontrola 25. 10. 2026

Kritický ani vážný nález nebyl. Opraveno: upgrade ze schématu 4 smaže značku denního porovnání a staré bloby jdou pryč při prvním úklidu, nezapečetěná hlášení server při startu zapečetí; zaznamenané uvolnění po posledním otevření zprávu ukončí, i když blob ještě leží; zapečetěné hlášení se kóduje bez HTML escapování a vejde se i nejdelší povolené; hlášení jsou binární záznamy po 2944 bajtech s limitem 64 MiB; `takedown` hlásí `already ended` i u blobu, který už čeká na úklid; záznam po vypršení lease zase zmenší a spec nepřehání, co kopie serveru ukáže.

## Ověření

`TestStolenServerRevealsNothing` hledá v ukradené kopii i id ve všech kódováních, hlavičku obálky, důvod a kontakt hlášení a čas jemnější než čtvrthodina. Sada `storetest` pokrývá souběh claimů, lease, konce zpráv a úklid nad zapečetěnými záznamy. Časové testy AK21 a AK22 platí dál, protože neznámé id stojí stejnou práci jako špatný klíč. E2E testy beze změny klienta.
