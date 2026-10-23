# Plán: milník 5, offline zpráva v souboru `.nzo`

Datum: 24. 10. 2026. Stav: návrh čeká na rozhodnutí vlastníka OD-J1 až OD-J13. Zdroj: rešerše `docs/research/J-offline-nzo.md` (ověřeno 23. 10. 2026), návrh `docs/designs/nylzo-architecture-research.md` (model hrozeb, „Kvantové hrozby pro tento návrh“, OD19, OD26, sekce CLI) a milník 4 (`docs/plans/2026-10-23-milestone-4-cli.md`).

## Cíl

Vlastník chce, aby si dva lidé s nainstalovaným `nylzo` mohli předat zprávu, i s přílohou, offline: odesílatel ji zašifruje do jednoho souboru `.nzo`, přenese ho třeba na USB a příjemcova binárka ho otevře. Je to druhý způsob doručení vedle odkazu a stejná binárka `nylzo` ho zvládne bez serveru.

Hotovo znamená: specifikace `spec/nzo.md` s testovacími vektory generovanými z Go, příkazy CLI, fuzzing parseru, mutační kontrola bezpečnostních pravidel, e2e mezi dvěma oddělenými konfiguracemi a nezávislé posouzení před vydáním.

## Co slíbit a co ne

- Nepublikovat „ani NSA ho nerozšifruje“; nejde to dokázat. Pravdivě jde říct, že soubor používá parametry, které NSA v sadě CNSA 2.0 uvádí pro národní bezpečnostní systémy (AES-256, ML-KEM-1024, ML-DSA-87, SHA-512), a že útok přímo na šifru je podle dnešních veřejných znalostí mimo dosah.
- Skutečné útoky vedou jinudy: slabá passphrase, napadený počítač odesílatele nebo příjemce, podvržený veřejný klíč při výměně, metadata na USB a zbytky plaintextu na disku.
- Offline nejde zajistit jednorázové otevření, expiraci, zrušení, spálení ani oznámení o otevření. Kdo má soubor a klíč, otevře ho kdykoli. Jediná poctivá náhrada je jednorázový klíč příjemce, který příjemce po otevření smaže.

## Doporučený návrh (podle rešerše J)

- Vlastní kontejner `.nzo` v1 jen ze standardních bloků standardní knihovny Go 1.27.3: HPKE (RFC 9180) s hybridním KEM MLKEM1024-P384, náhodný 256bitový klíč souboru, obsah v blocích po 64 KiB s AES-256-GCM a příznakem posledního bloku, HKDF-SHA-512 a HMAC-SHA-512 pro hlavičku. Jedna pevná sada algoritmů na verzi, žádné vyjednávání. Nový Go modul není potřeba.
- Výchozí předání klíče: veřejný klíč příjemce („identita“), jehož otisk si oba porovnají osobně nebo jiným ověřeným kanálem. V tomto režimu je povinný podpis odesílatele (ML-DSA-87 a ECDSA P-384, složený podpis), uložený uvnitř šifry a vázaný na hlavičku a na seznam příjemců.
- Další režimy: náhodný klíč souboru předaný druhým kanálem a vygenerovaná passphrase z nejméně 10 slov (zhruba 129 bitů) s Argon2id. Volitelně kombinace klíče příjemce s druhým tajemstvím.
- Padding PADMÉ s minimem 64 KiB; název a typ přílohy jen uvnitř šifry; meta JSON stejný jako v protokolu v1.
- CLI podle pravidel milníku 4 (žádná tajemství v argumentech, výstup do soukromého adresáře): `nylzo keygen`, `nylzo contact add/list/remove`, `nylzo seal`, `nylzo open`, `nylzo inspect`.
- Nejdřív jen CLI. Prohlížeč později a nejspíš jen pro režim s klíčem souboru, protože ML-KEM a ML-DSA ve Web Crypto nejsou.
- Alternativa age v1.3 je zralejší a interoperabilní, ale parametry CNSA 2.0 nesplní (klíč souboru 128 bitů, ML-KEM-768, bez podpisu a paddingu) a přidala by tři moduly.

## Rozhodnutí pro vlastníka

Podrobně v kapitole 12 rešerše J. Doporučení v závorce.

- OD-J1 formát (vlastní kontejner; age jen při přednosti interoperability)
- OD-J2 sada KEM (MLKEM1024-P384)
- OD-J3 výchozí předání klíče (klíč příjemce, k tomu klíč souboru a passphrase jako volby)
- OD-J4 podpis odesílatele (povinný u klíče příjemce, jinak volitelný)
- OD-J5 podpisová sada (ML-DSA-87 a ECDSA P-384)
- OD-J6 KDF pro passphrase a klíč v klidu (Argon2id)
- OD-J7 padding (PADMÉ, minimum 64 KiB)
- OD-J8 jednorázové klíče příjemce (jednotlivě v první verzi)
- OD-J9 prohlížeč (ne v první verzi)
- OD-J10 viditelná hlavička (magic a verze)
- OD-J11 kombinovaný režim (volitelně, po posouzení)
- OD-J12 expirace v souboru (nenabízet)
- OD-J13 vztah k OD19 a OD26 (jedna identita a jedna sada KEM i pro budoucí serverový režim na klíč příjemce)

## Postup po rozhodnutí

1. Dokumenty: návrh a `spec/nzo.md` s modelem hrozeb a vektory.
2. Jádro formátu v `internal/nzo`: hlavička, stanzy příjemců, bloky, padding, podpis, limity a pořadí kontrol; fuzzing parseru.
3. Identity a kontakty: uložení soukromého klíče v konfiguračním adresáři s právy 0600, šifrované Argon2id; otisky.
4. Příkazy CLI a testy včetně převrácených bitů, limitů a pokusu o přeposlání podepsané zprávy jinému příjemci.
5. Nezávislé posouzení kryptografie před vydáním.

Odhad: 20 až 26 člověkodnů bez externího posouzení; prohlížeč pro režim s klíčem souboru dalších 5 až 8.
