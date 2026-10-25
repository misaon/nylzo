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
  0x1_0c, 0x1_0d, 0x1_0e, 0x1_0f, 0x1_1a, 0x1_1b, 0x1_47, 0x1_48, 0x1_58, 0x1_59, 0x1_60, 0x1_61, 0x1_64, 0x1_65,
  0x1_6e, 0x1_6f, 0x1_7d, 0x1_7e,
];
const FAMILIES = [
  { file: 'geist', name: 'Geist' },
  { file: 'geist-mono', name: 'Geist Mono' },
  { file: 'sora', name: 'Sora' },
] as const;

function base128(bytes: Buffer, position: number): [number, number] {
  let value = 0;
  for (let index = 0; index < 5; index++) {
    const byte = bytes[position + index] ?? 0;
    value = value * 128 + (byte & 0x7f);
    if ((byte & 0x80) === 0) {
      return [value, position + index + 1];
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
  let position = 48;
  for (let table = 0; table < count; table++) {
    const flags = file[position++] ?? 0;
    const known = flags & 0x3f;
    const tag = known === 63 ? file.toString('latin1', position, (position += 4)) : (KNOWN_TAGS[known] ?? '');
    const transform = flags >> 6;
    let length: number;
    [length, position] = base128(file, position);
    if ((tag === 'glyf' || tag === 'loca') === (transform === 0)) {
      [length, position] = base128(file, position);
    }
    entries.push({ tag, length });
  }
  const data = brotliDecompressSync(file.subarray(position, position + compressed));
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
  const subtables = Array.from({ length: cmap.readUInt16BE(2) }, (_, index) => cmap.readUInt32BE(4 + index * 8 + 4));
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
  for (let segment = 0; segment < segments; segment++) {
    const end = cmap.readUInt16BE(ends + segment * 2);
    const start = cmap.readUInt16BE(starts + segment * 2);
    const delta = cmap.readInt16BE(deltas + segment * 2);
    const range = cmap.readUInt16BE(ranges + segment * 2);
    for (let code = start; code !== 0xff_ff && code <= end; code++) {
      const glyph =
        range === 0 ? (code + delta) & 0xff_ff : cmap.readUInt16BE(ranges + segment * 2 + range + (code - start) * 2);
      if (glyph !== 0) {
        mapped.push(code);
      }
    }
  }
  return mapped;
}

function codePoints(range: string): number[] {
  return range.split(',').flatMap((part) => {
    const [from = '', to = from] = part.trim().replace(/^U\+/, '').split('-', 2);
    const first = Number.parseInt(from, 16);
    return Array.from({ length: Number.parseInt(to, 16) - first + 1 }, (_, offset) => first + offset);
  });
}

function faceRanges(css: string, family: string): Map<string, number[]> {
  const faces = new Map<string, number[]>();
  for (const [, body = ''] of css.matchAll(/@font-face\s*\{([^}]*)\}/g)) {
    const source = /src:\s*url\(['"]([^'"]+)['"]\)/.exec(body)?.[1] ?? '';
    const range = /unicode-range:\s*([^\s;][^;]*);/.exec(body)?.[1] ?? '';
    if (new RegExp(String.raw`font-family:\s*['"]${family}['"];`).test(body)) {
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
    expect(
      Array.from({ length: 0x2_bb - 0x1_00 }, (_, offset) => 0x1_00 + offset).filter((code) => !covered.has(code)),
    ).toEqual([]);
  });
});
