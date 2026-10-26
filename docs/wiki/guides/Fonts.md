# Fonts

The web client includes Czech subsets of Geist, Geist Mono, and Sora in [web/app/fonts](../../../web/app/fonts). They contain č, ď, ě, ň, ř, š, ť, ů, ž and their uppercase forms, which are absent from the Latin subsets. Each file is about 4–5 kB; loading the full Latin Extended subsets would cost about 15–17 kB per font. Other extended Latin characters still use the `latin-ext` files from `@fontsource-variable`.

The checked-in subsets were generated from `@fontsource-variable/*` version 5.3.0 using fontTools 4.60.1 with brotli 1.1.0. Run this from `web/app` to reproduce them:

```sh
for family in geist geist-mono sora; do
  pyftsubset node_modules/@fontsource-variable/$family/files/$family-latin-ext-wght-normal.woff2 \
    --unicodes=U+010C-010F,U+011A-011B,U+0147-0148,U+0158-0159,U+0160-0161,U+0164-0165,U+016E-016F,U+017D-017E \
    --layout-features='*' --name-IDs='*' --flavor=woff2 --output-file=fonts/$family-czech.woff2
done
```

Regenerate the subsets after updating the font packages. The font tests check that their source version and character coverage match the installed packages.

The fonts use SIL Open Font License 1.1 with no reserved font names. Keep the copyright and license notices in `OFL-geist.txt`, `OFL-geist-mono.txt`, and `OFL-sora.txt` alongside the font files.
