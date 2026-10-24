# Česká písmena písem

Podmnožiny písem Geist, Geist Mono a Sora jen s českými písmeny, která nejsou v sadě `latin`: č, ď, ě, ň, ř, š, ť, ů, ž a jejich velké varianty (D86). Česká stránka tak místo tří sad `latin-ext` po 15 až 17 kB stáhne tři soubory po 4 až 5 kB. Ostatní znaky rozšířené latinky dál obsluhují sady `latin-ext` z balíčků `@fontsource-variable`. `src/app.css` z nich česká písmena vyjímá.

Soubory vznikly z balíčků `@fontsource-variable/*` ve verzi 5.3.0 nástrojem fontTools 4.60.1 (s modulem brotli 1.1.0). Příkaz spuštěný ve `web/app` dává bajtově stejné soubory:

```bash
for family in geist geist-mono sora; do
  pyftsubset node_modules/@fontsource-variable/$family/files/$family-latin-ext-wght-normal.woff2 \
    --unicodes=U+010C-010F,U+011A-011B,U+0147-0148,U+0158-0159,U+0160-0161,U+0164-0165,U+016E-016F,U+017D-017E \
    --layout-features='*' --name-IDs='*' --flavor=woff2 --output-file=fonts/$family-czech.woff2
done
```

Po aktualizaci balíčků písem je potřeba soubory vytvořit znovu. `test/fonts.test.ts` selže, když podmnožina pochází z jiné verze písma než balíček nebo nepokrývá právě tato písmena.

Písma jsou pod licencí SIL Open Font License 1.1 bez vyhrazených názvů písma. Licence s copyrightem autorů jsou v `OFL-geist.txt`, `OFL-geist-mono.txt` a `OFL-sora.txt`.
