# Plán: ukončení nahlášené zprávy provozovatelem

Datum: 24. 10. 2026. Větev: `claude/takedown`, postavená na `claude/transparency` (PR 10). Zdroj pravdy: design, sekce „Ochrana proti zneužití“ (ruční posouzení, reakce do 24 h, nikdy automatické smazání) a P7 (veřejná instance je podle DSA hostingová služba s mechanismem oznámení od prvního dne), `spec/protocol.md` §6.6 a §7. Stav: vlastník 24. 10. 2026 napsal, ať přes noc samostatně udělám co nejvíc bodů specifikace v pořadí.

## Problém

Provozovatel hlášení posoudí, ale zprávu ukončit neumí. Burn potřebuje klíč z odkazu, cancel správcovský klíč, a provozovatel nemá ani jeden. Hostingová služba přitom musí umět na oznámení zareagovat.

## D63: `nylzo takedown`

- Běží na stroji se serverem jako `sweep-once`: čte `NYLZO_DATA_DIR`, pepper nepotřebuje a databázi nemigruje. Funguje i vedle běžícího serveru, SQLite ve WAL to unese stejně jako u `sweep-once`.
- Id zpráv čte ze stdin, jedno na řádek, nejvýš 1 MiB a 10 000 id. Nejdřív zkontroluje všechny řádky. Chybný řádek nebo vložený odkaz s klíčem odmítne kódem 2 dřív, než cokoli změní, a v chybě vstup neopakuje. Terminál jako stdin odmítne.
- Každou zprávu ukončí stejnou cestou jako burn (`store.EndMessage` se stavem `deleted`): smaže blob a zapíše náhrobek. Příjemce uvidí, že zpráva byla smazána, a odesílatel ve stavu na `/sent` stav `deleted`. Text stavu proto neříká, že ji smazal příjemce, ale že ji smazal příjemce, nebo ji po hlášení odstranil provozovatel; stejně to uvádí stránka transparentnosti. Stažení, které už běží, může doběhnout, stejně jako u burn.
- Pro každé id vypíše výsledek: `deleted`, `no live message`, `already ended as …` nebo `failed: …`. Do logu serveru nepíše nic, takže id zůstane jen v terminálu provozovatele.

Ruční posouzení zůstává, příkaz nic nevybírá sám. Kdo má odkaz, může zprávu smazat i sám, takže takedown slouží hlavně pro oznámení bez odkazu, třeba od úřadu, a pro zprávy, které ohlašovatel nesmazal.

## Na vlastníkovi

- Zda má příjemce po zásahu provozovatele vidět jiný stav než „smazáno“, třeba „odstraněno provozovatelem“, a jak splnit odůvodnění podle čl. 17 DSA u služby bez účtů. Jiný stav by byl nová hodnota v protokolu (spec §11).
