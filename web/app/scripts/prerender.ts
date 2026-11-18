import { createHash } from 'node:crypto';
import { existsSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

import { checkScript, checkStyles, secureHtml } from './resource-integrity.ts';

interface HomeAssets {
  readonly image: { readonly path: string; readonly width: number; readonly height: number };
}

interface Prerender {
  readonly homeTemplates: (index: string, assets: HomeAssets) => ReadonlyMap<string, string>;
  readonly agentsMarkdown: () => string;
  readonly llmsText: () => string;
  readonly statusTemplates: (index: string) => ReadonlyMap<string, string>;
}

const root = path.resolve(import.meta.dirname, '..');
const distribution = path.resolve(root, '../../internal/server/web/dist');
const assets = path.join(distribution, 'assets');

function hashedName(prefix: string, extension: string, data: Buffer | string): string {
  return `${prefix}-${createHash('sha256').update(data).digest('base64url').slice(0, 8)}.${extension}`;
}

function jpegSize(data: Buffer): { readonly width: number; readonly height: number } {
  for (let offset = 2; offset + 9 <= data.length; offset += 2 + data.readUInt16BE(offset + 2)) {
    const marker = data.readUInt8(offset + 1);
    if (data.readUInt8(offset) !== 0xff) {
      break;
    }
    if (marker >= 0xc0 && marker <= 0xcf && marker !== 0xc4 && marker !== 0xc8 && marker !== 0xcc) {
      return { height: data.readUInt16BE(offset + 5), width: data.readUInt16BE(offset + 7) };
    }
  }
  throw new Error('og-image.jpg has no frame header');
}

const imageFile = readFileSync(path.join(root, 'icons/og-image.jpg'));
const imageName = hashedName('og-image', 'jpg', imageFile);
writeFileSync(path.join(assets, imageName), imageFile);
const image = { path: `/assets/${imageName}`, ...jpegSize(imageFile) };

// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- This module was generated from our typed prerender entry point by the preceding Vite SSR build.
const prerender = (await import(pathToFileURL(path.join(root, '.prerender/prerender.js')).href)) as Prerender;
const index = readFileSync(path.join(distribution, 'index.html'), 'utf8');
const pages = prerender.homeTemplates(index, { image });
for (const [language, page] of pages) {
  writeFileSync(path.join(distribution, `home.${language}.html`), page);
}
writeFileSync(path.join(distribution, 'agents.md'), prerender.agentsMarkdown());
writeFileSync(path.join(distribution, 'llms.txt'), prerender.llmsText());
const statusPages = prerender.statusTemplates(index);
for (const [name, page] of statusPages) {
  writeFileSync(path.join(distribution, name), page);
}

const bundled = new Map(
  readdirSync(assets)
    .filter((name) => !name.endsWith('.gz'))
    .map((name) => [`/assets/${name}`, readFileSync(path.join(assets, name))]),
);
for (const [name, data] of bundled) {
  if (/\.(?:js|css)$/u.test(name) && !existsSync(path.join(distribution, `${name}.gz`))) {
    throw new Error(`prerender: ${name} has no gzip variant, so the server would send it uncompressed`);
  }
  if (name.endsWith('.js')) {
    checkScript(data.toString(), name, bundled);
  }
  if (name.endsWith('.css')) {
    checkStyles(data.toString(), name, bundled);
  }
}
// The admin page and the public pages name only their own scripts in their import maps.
const ADMIN_ASSET = '/assets/admin-';
const publicAssets = new Map([...bundled].filter(([name]) => !name.startsWith(ADMIN_ASSET)));
const adminAssets = new Map([...bundled].filter(([name]) => name.startsWith(ADMIN_ASSET) || !name.endsWith('.js')));
const homePages = [...pages.keys()].map((language) => `home.${language}.html`);
for (const name of ['index.html', ...homePages, ...statusPages.keys()]) {
  const file = path.join(distribution, name);
  writeFileSync(file, secureHtml(readFileSync(file, 'utf8'), publicAssets));
}
const adminPage = path.join(distribution, 'admin.html');
writeFileSync(adminPage, secureHtml(readFileSync(adminPage, 'utf8'), adminAssets));
