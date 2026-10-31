# Brand assets

The official SVG files are in [assets/brand](../../../assets/brand). Use `light/` on a light background and `dark/` on a dark background. Keep the original logo geometry and lettering. [Name and logo](Name-and-logo.md) explains the permitted use of the brand.

| Variant                 | Artwork                | View box  |
| ----------------------- | ---------------------- | --------- |
| `nylzo-landscape-*.svg` | Symbol beside the name | 430 × 150 |
| `nylzo-square-*.svg`    | Symbol above the name  | 320 × 320 |
| `nylzo-icon-*.svg`      | Symbol only            | 96 × 96   |

Files ending in `-background.svg` have an opaque white (`#FFFFFF`) or navy (`#080D20`) background. Other variants have a transparent canvas. The name uses vector paths and does not depend on an installed font. [manifest.json](../../../assets/brand/manifest.json) records the colors and formats.

## Export icons

Use `rsvg-convert` from librsvg to export a PNG. For example, from the repository root:

```sh
rsvg-convert -w 180 -h 180 -o apple-touch-icon.png assets/brand/dark/nylzo-icon-dark-background.svg
```

The web client uses the dark icon variants in [web/app/icons](../../../web/app/icons). Its SVG favicon matches the original logo, and its PNG icons are rendered at 32 and 180 pixels.

The social preview image `og-image.png` is rendered from `og-image.svg` in the same folder. With librsvg 2.63.2 this command reproduces the committed file byte for byte:

```sh
rsvg-convert -w 1200 -h 630 -o web/app/icons/og-image.png web/app/icons/og-image.svg
```

## README covers

[assets/readme](../../../assets/readme) contains matching light and dark SVG covers using the original logo geometry and theme colors. The sealed-message artwork illustrates sharing and expiry; it is not a product screenshot or a security certification.

The covers are self-contained SVG files with accessible descriptions. They use opaque artwork, with no scripts, animation, embedded HTML, remote assets, or custom font downloads. Keep their compositions aligned when editing them.
