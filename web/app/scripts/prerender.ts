import { createHash } from 'node:crypto';
import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

import { checkScript, checkStyles, secureHtml } from './resource-integrity.ts';

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

const imageFile = readFileSync(path.join(root, 'icons/og-image.png'));
const imageName = hashedName('og-image', 'png', imageFile);
writeFileSync(path.join(assets, imageName), imageFile);
const image = {
  path: `/assets/${imageName}`,
  width: imageFile.readUInt32BE(16),
  height: imageFile.readUInt32BE(20),
};

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

// Seal every final template, including the font stylesheet created above.
const bundled = new Map(
  readdirSync(assets)
    .filter((name) => !name.endsWith('.gz'))
    .map((name) => [`/assets/${name}`, readFileSync(path.join(assets, name))]),
);
for (const [name, data] of bundled) {
  if (name.endsWith('.js')) {
    checkScript(data.toString(), name, bundled);
  }
  if (name.endsWith('.css')) {
    checkStyles(data.toString(), name, bundled);
  }
}
for (const name of ['index.html', 'home.en.html', 'home.cs.html']) {
  const file = path.join(distribution, name);
  writeFileSync(file, secureHtml(readFileSync(file, 'utf8'), bundled));
}
