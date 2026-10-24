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

interface Prerender {
  readonly LANGUAGES: readonly Language[];
  homeTemplate(index: string, language: Language, image: OgImage): string;
}

const root = resolve(import.meta.dirname, '..');
const dist = resolve(root, '../../internal/server/web/dist');

const png = readFileSync(join(root, 'icons/og-image.png'));
const name = `og-image-${createHash('sha256').update(png).digest('base64url').slice(0, 8)}.png`;
writeFileSync(join(dist, 'assets', name), png);
const image: OgImage = { path: `/assets/${name}`, width: png.readUInt32BE(16), height: png.readUInt32BE(20) };

const { LANGUAGES, homeTemplate } = (await import(pathToFileURL(join(root, '.prerender/prerender.js')).href)) as Prerender;
const index = readFileSync(join(dist, 'index.html'), 'utf8');
for (const language of LANGUAGES) {
  writeFileSync(join(dist, `home.${language}.html`), homeTemplate(index, language, image));
}
