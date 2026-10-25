import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

interface HomeAssets {
  readonly image: { readonly path: string; readonly width: number; readonly height: number };
  readonly styles: { readonly stylesheet: string; readonly fontStylesheet: string; readonly inline: string };
}

interface Prerender {
  readonly splitFontFaces: (css: string) => { readonly fontFaces: string; readonly inline: string };
  readonly homeTemplates: (index: string, assets: HomeAssets) => ReadonlyMap<string, string>;
}

const root = path.resolve(import.meta.dirname, '..');
const distribution = path.resolve(root, '../../internal/server/web/dist');
const assets = path.join(distribution, 'assets');

function hashedName(prefix: string, extension: string, data: Buffer | string): string {
  return `${prefix}-${createHash('sha256').update(data).digest('base64url').slice(0, 8)}.${extension}`;
}

const png = readFileSync(path.join(root, 'icons/og-image.png'));
const imageName = hashedName('og-image', 'png', png);
writeFileSync(path.join(assets, imageName), png);
const image = { path: `/assets/${imageName}`, width: png.readUInt32BE(16), height: png.readUInt32BE(20) };

const prerender = (await import(pathToFileURL(path.join(root, '.prerender/prerender.js')).href)) as Prerender;
const index = readFileSync(path.join(distribution, 'index.html'), 'utf8');
const stylesheet = /<link rel="stylesheet"[^>]* href="(\/assets\/[^"]+\.css)"/.exec(index)?.[1] ?? '';
const { fontFaces, inline } = prerender.splitFontFaces(readFileSync(path.join(distribution, stylesheet), 'utf8'));
const fontStylesheet = hashedName('fonts', 'css', fontFaces);
writeFileSync(path.join(assets, fontStylesheet), fontFaces);
const styles = { stylesheet, fontStylesheet: `/assets/${fontStylesheet}`, inline };
const pages = prerender.homeTemplates(index, { image, styles });
for (const [language, page] of pages) {
  writeFileSync(path.join(distribution, `home.${language}.html`), page);
}
