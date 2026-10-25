import { readFileSync } from 'node:fs';
import { brotliDecompressSync } from 'node:zlib';

import { describe, expect, it } from 'vitest';

const KNOWN_TAGS = [
  'cmap',
  'head',
  'hhea',
  'hmtx',
  'maxp',
  'name',
  'OS/2',
  'post',
  'cvt ',
  'fpgm',
  'glyf',
  'loca',
  'prep',
  'CFF ',
  'VORG',
  'EBDT',
  'EBLC',
  'gasp',
  'hdmx',
  'kern',
  'LTSH',
  'PCLT',
  'VDMX',
  'vhea',
  'vmtx',
  'BASE',
  'GDEF',
  'GPOS',
  'GSUB',
  'EBSC',
  'JSTF',
  'MATH',
  'CBDT',
  'CBLC',
  'COLR',
  'CPAL',
  'SVG ',
  'sbix',
  'acnt',
  'avar',
  'bdat',
  'bloc',
  'bsln',
  'cvar',
  'fdsc',
  'feat',
  'fmtx',
  'fvar',
  'gvar',
  'hsty',
  'just',
  'lcar',
  'mort',
  'morx',
  'opbd',
  'prop',
  'trak',
  'Zapf',
  'Silf',
  'Glat',
  'Gloc',
  'Feat',
  'Sill',
];
const CZECH = [
  0x10c, 0x10d, 0x10e, 0x10f, 0x11a, 0x11b, 0x147, 0x148, 0x158, 0x159, 0x160, 0x161, 0x164, 0x165, 0x16e, 0x16f, 0x17d,
  0x17e,
];
const FAMILIES = [
  { file: 'geist', name: 'Geist' },
  { file: 'geist-mono', name: 'Geist Mono' },
  { file: 'sora', name: 'Sora' },
] as const;

function base128(bytes: Buffer, at: number): [number, number] {
  let value = 0;
  for (let i = 0; i < 5; i++) {
    const byte = bytes[at + i] ?? 0;
    value = value * 128 + (byte & 0x7f);
    if ((byte & 0x80) === 0) {
      return [value, at + i + 1];
    }
  }
  throw new Error('woff2: bad UIntBase128');
}

function woff2Tables(path: string): Map<string, Buffer> {
  const file = readFileSync(path);
  if (file.toString('latin1', 0, 4) !== 'wOF2') {
    throw new Error(`${path} is not WOFF2`);
  }
  const count = file.readUInt16BE(12);
  const compressed = file.readUInt32BE(20);
  const entries: { tag: string; length: number }[] = [];
  let at = 48;
  for (let i = 0; i < count; i++) {
    const flags = file[at++] ?? 0;
    const known = flags & 0x3f;
    const tag = known === 63 ? file.toString('latin1', at, (at += 4)) : (KNOWN_TAGS[known] ?? '');
    const transform = flags >> 6;
    let length: number;
    [length, at] = base128(file, at);
    if ((tag === 'glyf' || tag === 'loca') === (transform === 0)) {
      [length, at] = base128(file, at);
    }
    entries.push({ tag, length });
  }
  const data = brotliDecompressSync(file.subarray(at, at + compressed));
  const tables = new Map<string, Buffer>();
  let offset = 0;
  for (const { tag, length } of entries) {
    tables.set(tag, data.subarray(offset, offset + length));
    offset += length;
  }
  return tables;
}

function fontRevision(path: string): number {
  const head = woff2Tables(path).get('head');
  if (!head) {
    throw new Error(`${path} has no head table`);
  }
  return head.readUInt32BE(4);
}

function mappedCodePoints(path: string): number[] {
  const cmap = woff2Tables(path).get('cmap');
  if (!cmap) {
    throw new Error(`${path} has no cmap table`);
  }
  const subtables = Array.from({ length: cmap.readUInt16BE(2) }, (_, i) => cmap.readUInt32BE(4 + i * 8 + 4));
  const format4 = subtables.find((offset) => cmap.readUInt16BE(offset) === 4);
  if (format4 === undefined) {
    throw new Error(`${path} has no format 4 cmap`);
  }
  const segments = cmap.readUInt16BE(format4 + 6) / 2;
  const ends = format4 + 14;
  const starts = ends + segments * 2 + 2;
  const deltas = starts + segments * 2;
  const ranges = deltas + segments * 2;
  const mapped: number[] = [];
  for (let s = 0; s < segments; s++) {
    const end = cmap.readUInt16BE(ends + s * 2);
    const start = cmap.readUInt16BE(starts + s * 2);
    const delta = cmap.readInt16BE(deltas + s * 2);
    const range = cmap.readUInt16BE(ranges + s * 2);
    for (let code = start; code <= end && code !== 0xffff; code++) {
      const glyph =
        range === 0 ? (code + delta) & 0xffff : cmap.readUInt16BE(ranges + s * 2 + range + (code - start) * 2);
      if (glyph !== 0) {
        mapped.push(code);
      }
    }
  }
  return mapped;
}

function codePoints(range: string): number[] {
  return range.split(',').flatMap((part) => {
    const [from = '', to = from] = part.trim().replace(/^U\+/, '').split('-');
    const first = Number.parseInt(from, 16);
    return Array.from({ length: Number.parseInt(to, 16) - first + 1 }, (_, i) => first + i);
  });
}

function faceRanges(css: string, family: string): Map<string, number[]> {
  const faces = new Map<string, number[]>();
  for (const [, body = ''] of css.matchAll(/@font-face\s*\{([^}]*)\}/g)) {
    const source = /src:\s*url\(['"]([^'"]+)['"]\)/.exec(body)?.[1] ?? '';
    const range = /unicode-range:\s*([^;]+);/.exec(body)?.[1] ?? '';
    if (new RegExp(`font-family:\\s*['"]${family}['"];`).test(body)) {
      faces.set(source, codePoints(range));
    }
  }
  return faces;
}

describe('Czech font subsets (D86)', () => {
  const css = readFileSync(new URL('../app.css', import.meta.url), 'utf8');

  it.each(FAMILIES)('$name comes from the same font release as the packaged latin-ext file', ({ file }) => {
    const packaged = new URL(
      `../../node_modules/@fontsource-variable/${file}/files/${file}-latin-ext-wght-normal.woff2`,
      import.meta.url,
    ).pathname;
    const subset = new URL(`../../fonts/${file}-czech.woff2`, import.meta.url).pathname;
    expect(fontRevision(subset)).toBe(fontRevision(packaged));
    expect(mappedCodePoints(subset)).toEqual(CZECH);
  });

  it.each(FAMILIES)('$name sends Czech letters to the subset and nothing else twice', ({ file, name }) => {
    const faces = faceRanges(css, name);
    const czech = faces.get(`../fonts/${file}-czech.woff2`);
    const extended =
      faces.get(`../node_modules/@fontsource-variable/${file}/files/${file}-latin-ext-wght-normal.woff2`) ?? [];
    expect(czech).toEqual(CZECH);
    expect(extended.filter((code) => CZECH.includes(code))).toEqual([]);
    const covered = new Set([...extended, ...CZECH]);
    expect(Array.from({ length: 0x2bb - 0x100 }, (_, i) => 0x100 + i).filter((code) => !covered.has(code))).toEqual([]);
  });
});
