# Brand

This page is for anyone who names Nylzo or shows its logo, and for contributors who change the web client's icons or fonts.

## Name and logo

The [MIT license](../../../LICENSE) grants no rights to the Nylzo name or logo. You may use the name to refer to this project. Do not use the name or logo to brand another service or a modified version, or to suggest a connection with [nylzo.com](https://nylzo.com).

## Logo files

The official SVG files are in [assets/brand](../../../assets/brand). Use `light/` on light backgrounds and `dark/` on dark ones, and keep the logo's shape and lettering unchanged.

| File | Artwork | View box |
| --- | --- | --- |
| `nylzo-landscape-*.svg` | Symbol beside the name | 430 × 150 |
| `nylzo-square-*.svg` | Symbol above the name | 320 × 320 |
| `nylzo-icon-*.svg` | Symbol only | 96 × 96 |

Files ending in `-background.svg` have a solid background, white `#FFFFFF` in the light set and navy `#080D20` in the dark set; the others are transparent. The lettering is vector paths and needs no font. [manifest.json](../../../assets/brand/manifest.json) lists the text, background and gradient colors of both sets.

## Web client icons

[web/app/icons](../../../web/app/icons) holds `favicon.svg`, a copy of `dark/nylzo-icon-dark-background.svg`, its PNG renders and the social preview `og-image.svg`. [web/app/public/favicon.ico](../../../web/app/public/favicon.ico) serves clients that ask for `/favicon.ico`. To render the bitmaps again with librsvg and ImageMagick, from the repository root:

```sh
icon=assets/brand/dark/nylzo-icon-dark-background.svg
rsvg-convert -w 96 -h 96 -o web/app/icons/favicon-96.png "$icon"
rsvg-convert -w 180 -h 180 -o web/app/icons/apple-touch-icon.png "$icon"
for size in 16 32 48; do rsvg-convert -w "$size" -h "$size" -o "favicon-$size.png" "$icon"; done
magick favicon-16.png favicon-32.png favicon-48.png web/app/public/favicon.ico
rsvg-convert -w 1200 -h 630 -o web/app/icons/og-image.png web/app/icons/og-image.svg
```

## Fonts

The web client uses Geist and Sora from the `@fontsource-variable` packages, under the SIL Open Font License 1.1. Keep `OFL-geist.txt` and `OFL-sora.txt` next to the font files.

[web/app/fonts](../../../web/app/fonts) adds small subsets with only the Czech letters missing from the Latin set (č, ď, ě, ň, ř, š, ť, ů, ž and their capitals), so Czech pages load about 5 kB per font instead of the full Latin Extended file. After updating the font packages, regenerate them from `web/app` with fontTools:

```sh
for family in geist sora; do
  pyftsubset node_modules/@fontsource-variable/$family/files/$family-latin-ext-wght-normal.woff2 \
    --unicodes=U+010C-010F,U+011A-011B,U+0147-0148,U+0158-0159,U+0160-0161,U+0164-0165,U+016E-016F,U+017D-017E \
    --layout-features='*' --name-IDs='*' --flavor=woff2 --output-file=fonts/$family-czech.woff2
done
```

The font tests fail when a subset comes from another font version or covers other characters.
