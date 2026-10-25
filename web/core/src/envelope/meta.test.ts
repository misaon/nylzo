import { describe, expect, it } from 'vitest';

import { vectors } from '../../test/vectors.ts';
import { MetaError } from '../errors.ts';
import { MAX_FILE_BYTES, MAX_META_BYTES, MAX_TEXT_BYTES } from '../limits.ts';
import { concatenate, utf8 } from '../primitives/bytes.ts';
import { decodeMeta, encodeMeta, type Meta } from './meta.ts';

function decodeText(json: string): Meta {
  return decodeMeta(utf8(json));
}

function fileMeta(name: string, mime = '', size = 1): Meta {
  return { file: { name, mime, size } };
}

function padded(length: number): string {
  return `{"v":1,"text":"x","pad":"${'p'.repeat(length)}"}`;
}

describe('AK9: meta vectors', () => {
  const valid = vectors.meta.filter((vector) => vector.valid);
  const invalid = vectors.meta.filter((vector) => !vector.valid);

  it.each(valid)('$name', (vector) => {
    const meta = decodeMeta(utf8(vector.input));
    const canonical = encodeMeta(meta);
    expect(new TextDecoder().decode(canonical)).toBe(vector.canonical);
    const file =
      vector.file_name === undefined
        ? undefined
        : { name: vector.file_name, mime: vector.file_mime, size: vector.file_size };
    expect(meta).toEqual({ text: vector.text, file });
    expect(encodeMeta(decodeMeta(canonical))).toEqual(canonical);
  });

  it.each(invalid)('$name is rejected', (vector) => {
    expect(() => decodeMeta(utf8(vector.input))).toThrow(MetaError);
  });
});

describe('meta encoding (spec 3.1)', () => {
  it('counts the limits in UTF-8 bytes', () => {
    expect(() => encodeMeta({ text: 'é'.repeat(MAX_TEXT_BYTES / 2) })).not.toThrow();
    expect(() => encodeMeta(fileMeta('é'.repeat(127)))).not.toThrow();
    expect(() => encodeMeta(fileMeta('a b.čé (1).txt', '', MAX_FILE_BYTES))).not.toThrow();
    expect(encodeMeta({ text: 'x'.repeat(MAX_TEXT_BYTES) })).toHaveLength(MAX_TEXT_BYTES + 17);
  });

  it.each<[string, Meta]>([
    ['neither text nor file', {}],
    ['text over the limit in UTF-8 bytes', { text: 'é'.repeat(MAX_TEXT_BYTES / 2 + 1) }],
    ['text whose escaping overflows the meta limit', { text: '"'.repeat(60_000) }],
    ['a file name over the limit in UTF-8 bytes', fileMeta('é'.repeat(128))],
    ['a mime type over the limit in UTF-8 bytes', fileMeta('a', 'é'.repeat(128))],
    ['a lone surrogate in the text', { text: 'a\u{D800}' }],
    ['a lone surrogate in the file name', fileMeta('\u{DC00}')],
    ['a lone surrogate in the mime type', fileMeta('a', '\u{D800}')],
    ['a fractional size', fileMeta('a', '', 1.5)],
    ['a size that is not a number', fileMeta('a', '', NaN)],
  ])('rejects %s', (_, meta) => {
    expect(() => encodeMeta(meta)).toThrow(MetaError);
  });
});

describe('meta decoding (spec 3.1)', () => {
  it.each<[string, Meta]>([
    ['{"v":1,"text":"x","extra":{"nested":[1,2.5e3,-0,true,false,null,"s",{}],"o":{}}}', { text: 'x' }],
    ['{"v":2,"text":"x","v":1}', { text: 'x' }],
    ['{"v":1,"text":"x","file":null}', { text: 'x' }],
    [String.raw`{"v":1,"text":"a\/béé😀\"\\\b\f\n\r\t"}`, { text: 'a/béé😀"\\\b\f\n\r\t' }],
    [String.raw`{"v":1,"text":"\ud800x"}`, { text: '\u{FFFD}x' }],
    [String.raw`{"v":1,"text":"\udc00"}`, { text: '\u{FFFD}' }],
    [String.raw`{"v":1,"text":"\ud800\ud800"}`, { text: '\u{FFFD}\u{FFFD}' }],
    [String.raw`{"v":1,"text":"\ud800A"}`, { text: '\u{FFFD}A' }],
  ])('decodes %s as the Go core does', (json, meta) => {
    expect(decodeText(json)).toEqual(meta);
  });

  it.each([
    '',
    ' ',
    '"x"',
    '1',
    'null',
    '{"v":1,"text":"x"}x',
    '\u{FEFF}{"v":1,"text":"x"}',
    '{"v":1,"text":"x",}',
    '{"v":1,"text":"x"',
    '{"v":1,"text":"x\n"}',
    String.raw`{"v":1,"text":"x\x"}`,
    String.raw`{"v":1,"text":"\u12"}`,
    String.raw`{"v":1,"text":"\u12G4"}`,
    `{"v":1,"text":'x'}`,
    '{v:1,"text":"x"}',
    '{"v":01,"text":"x"}',
    '{"v":1.,"text":"x"}',
    '{"v":+1,"text":"x"}',
    '{"v":1,"text":"x","extra":[1,]}',
    '{"v":1,"text":"x","extra":undefined}',
    '{"v":1,"text":"x","extra":NaN}',
  ])('rejects the malformed document %j', (json) => {
    expect(() => decodeText(json)).toThrow(MetaError);
  });

  it.each([
    '{"v":1e0,"text":"x"}',
    '{"v":-1,"text":"x"}',
    '{"v":true,"text":"x"}',
    '{"v":[1],"text":"x"}',
    '{"v":1,"text":1}',
    '{"v":1,"text":["x"]}',
    '{"v":1,"file":"x"}',
    '{"v":1,"file":[]}',
    '{"v":1,"file":{}}',
    '{"v":1,"file":{"name":"a"}}',
    '{"v":1,"file":{"name":null,"size":1}}',
    '{"v":1,"file":{"name":1,"size":1}}',
    '{"v":1,"file":{"name":"a","mime":1,"size":1}}',
    '{"v":1,"file":{"name":"a","size":null}}',
    '{"v":1,"file":{"name":"a","size":1e0}}',
    '{"v":1,"file":{"name":"a","size":true}}',
    '{"v":1,"file":{"name":"a","size":99999999999999999999}}',
  ])('rejects the wrong type or non-integer in %s', (json) => {
    expect(() => decodeText(json)).toThrow(MetaError);
  });

  it('rejects invalid UTF-8', () => {
    expect(() => decodeMeta(concatenate(utf8('{"v":1,"text":"'), Uint8Array.of(255), utf8('"}')))).toThrow(MetaError);
    expect(() => decodeMeta(Uint8Array.of(195))).toThrow(MetaError);
  });

  it('bounds the input to MAX_META_BYTES', () => {
    const overhead = padded(0).length;
    expect(() => decodeText(padded(MAX_META_BYTES - overhead))).not.toThrow();
    expect(() => decodeText(padded(MAX_META_BYTES - overhead + 1))).toThrow(MetaError);
  });
});
