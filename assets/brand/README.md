# Logo Nylzo

Oficiální sada loga od vlastníka projektu (24. 10. 2026, D72). Varianty v `light/` patří na světlé pozadí, varianty v `dark/` na tmavé.

| Soubor | Formát |
|---|---|
| `nylzo-landscape-*.svg` | značka a nápis vedle sebe, 430 × 150 |
| `nylzo-square-*.svg` | značka nad nápisem, 320 × 320 |
| `nylzo-icon-*.svg` | samotná značka N, 96 × 96 |

Soubory bez `-background` mají průhledné plátno. Soubory s `-background` mají plné pozadí, bílé `#FFFFFF` ve světlé variantě a tmavomodré `#080D20` v tmavé. Nápis je z vektorových tvarů, takže logo na písmu nezávisí. Barvy textu, pozadí a přechodu obou variant jsou v `manifest.json`.

Původní sada má navíc PNG exporty a ikony 16 až 512 px. Repozitář je nedrží, protože je vykreslí `rsvg-convert` (librsvg 2.63): `dark/nylzo-icon-dark-background.svg` v 1024 px vyjde pixelově shodně s PNG ze sady.

```bash
rsvg-convert -w 180 -h 180 -o apple-touch-icon.png dark/nylzo-icon-dark-background.svg
```

Webový klient je jen tmavý a ikonu má v `web/app/icons/`. `favicon.svg` je kopie `dark/nylzo-icon-dark-background.svg` (shodu hlídá `web/app/test/icons.test.ts`), `favicon-32.png` a `apple-touch-icon.png` jsou z ní vykreslené tímto příkazem ve 32 a 180 px.
