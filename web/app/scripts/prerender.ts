import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

type Language = 'en' | 'cs';

interface OgImage {
  readonly path: string;
  readonly width: number;
  readonly height: number;
}

interface HomeStyles {
  readonly stylesheet: string;
  readonly fontStylesheet: string;
  readonly inline: string;
}

interface HomeAssets {
  readonly image: OgImage;
  readonly styles: HomeStyles;
}

interface Prerender {
  readonly LANGUAGES: readonly Language[];
  splitFontFaces(css: string): { readonly fontFaces: string; readonly inline: string };
  homeTemplate(index: string, language: Language, assets: HomeAssets): string;
}

const root = resolve(import.meta.dirname, '..');
const dist = resolve(root, '../../internal/server/web/dist');

function hashed(prefix: string, extension: string, data: Buffer | string): string {
  return `${prefix}-${createHash('sha256').update(data).digest('base64url').slice(0, 8)}.${extension}`;
}

const png = readFileSync(join(root, 'icons/og-image.png'));
const imageName = hashed('og-image', 'png', png);
writeFileSync(join(dist, 'assets', imageName), png);
const image: OgImage = { path: `/assets/${imageName}`, width: png.readUInt32BE(16), height: png.readUInt32BE(20) };

const { LANGUAGES, splitFontFaces, homeTemplate } = (await import(pathToFileURL(join(root, '.prerender/prerender.js')).href)) as Prerender;
const index = readFileSync(join(dist, 'index.html'), 'utf8');
const stylesheet = /<link rel="stylesheet"[^>]* href="(\/assets\/[^"]+\.css)"/.exec(index)?.[1] ?? '';
const { fontFaces, inline } = splitFontFaces(readFileSync(join(dist, stylesheet), 'utf8'));
const fontStylesheet = hashed('fonts', 'css', fontFaces);
writeFileSync(join(dist, 'assets', fontStylesheet), fontFaces);
const styles: HomeStyles = { stylesheet, fontStylesheet: `/assets/${fontStylesheet}`, inline };
for (const language of LANGUAGES) {
  writeFileSync(join(dist, `home.${language}.html`), homeTemplate(index, language, { image, styles }));
}
