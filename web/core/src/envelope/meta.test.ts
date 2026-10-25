import { describe, expect, it } from 'vitest';

import {
  concatenate,
  decodeMeta,
  encodeMeta,
  MAX_FILE_BYTES,
  MAX_FILE_NAME,
  MAX_META_BYTES,
  MAX_MIME,
  MAX_TEXT_BYTES,
  type Meta,
  MetaError,
  utf8,
  validateMeta,
} from '../index.ts';

const decoder = new TextDecoder();

function canonical(meta: Meta): string {
  return decoder.decode(encodeMeta(meta));
}

function decodeText(json: string): Meta {
  return decodeMeta(utf8(json));
}

function fileMeta(name: string, mime: string): Meta {
  return { file: { name, mime, size: 1 } };
}

function sizedMeta(size: number): Meta {
  return { file: { name: 'a', mime: '', size } };
}

function padded(length: number): string {
  return `{"v":1,"text":"x","pad":"${'p'.repeat(length)}"}`;
}

const NAMED_ESCAPES = new Map([
  ['\b', String.raw`\b`],
  ['\t', String.raw`\t`],
  ['\n', String.raw`\n`],
  ['\f', String.raw`\f`],
  ['\r', String.raw`\r`],
]);

describe('canonical encoding', () => {
  it('applies the escaping table of the spec', () => {
    const text = 'q"b\\s\b\t\n\f\r\u{1}\u{1F}/é\u{2028}\u{2029}\u{7F}😀';
    expect(canonical({ text })).toBe(
      '{"v":1,"text":"q\\"b\\\\s\\b\\t\\n\\f\\r\\u0001\\u001f/é\u{2028}\u{2029}\u{7F}😀"}',
    );
  });

  it('escapes every other C0 control as lowercase \\u00xx', () => {
    for (let code = 0; code < 32; code++) {
      const character = String.fromCodePoint(code);
      const expected = NAMED_ESCAPES.get(character) ?? String.raw`\u00${code.toString(16).padStart(2, '0')}`;
      expect(canonical({ text: character })).toBe(`{"v":1,"text":"${expected}"}`);
    }
  });

  it('orders keys as v, text, file and name, mime, size', () => {
    expect(canonical({ file: { name: 'a.txt', mime: 'text/plain', size: 3 } })).toBe(
      '{"v":1,"file":{"name":"a.txt","mime":"text/plain","size":3}}',
    );
    expect(canonical({ text: 't', file: { name: 'a', mime: '', size: 0 } })).toBe(
      '{"v":1,"text":"t","file":{"name":"a","mime":"","size":0}}',
    );
    expect(canonical({ text: 'ahoj', file: undefined })).toBe('{"v":1,"text":"ahoj"}');
  });

  it('is a fixed point of decode and encode', () => {
    const metas: Meta[] = [
      { text: 'příloha "v" příloze\n' },
      { file: { name: 'příloha č.1.pdf', mime: 'application/pdf', size: 1000 } },
      { text: '\0\u{1F}\u{7F}', file: { name: ' ', mime: 'x/y; q="1"', size: MAX_FILE_BYTES } },
    ];
    for (const meta of metas) {
      const once = encodeMeta(meta);
      const twice = encodeMeta(decodeMeta(once));
      expect(once).toEqual(twice);
      expect(decodeMeta(twice)).toEqual(meta);
    }
  });
});

describe('limits', () => {
  it('bounds text by UTF-8 bytes', () => {
    expect(() => validateMeta({ text: 'x'.repeat(MAX_TEXT_BYTES) })).not.toThrow();
    expect(() => validateMeta({ text: 'x'.repeat(MAX_TEXT_BYTES + 1) })).toThrow(MetaError);
    expect(() => validateMeta({ text: 'é'.repeat(MAX_TEXT_BYTES / 2) })).not.toThrow();
    expect(() => validateMeta({ text: 'é'.repeat(MAX_TEXT_BYTES / 2 + 1) })).toThrow(MetaError);
  });

  it('bounds file name and mime by UTF-8 bytes', () => {
    expect(() => validateMeta(fileMeta('n'.repeat(MAX_FILE_NAME), ''))).not.toThrow();
    expect(() => validateMeta(fileMeta('n'.repeat(MAX_FILE_NAME + 1), ''))).toThrow(MetaError);
    expect(() => validateMeta(fileMeta('é'.repeat(127), ''))).not.toThrow();
    expect(() => validateMeta(fileMeta('é'.repeat(128), ''))).toThrow(MetaError);
    expect(() => validateMeta(fileMeta('a', 'm'.repeat(MAX_MIME)))).not.toThrow();
    expect(() => validateMeta(fileMeta('a', 'm'.repeat(MAX_MIME + 1)))).toThrow(MetaError);
    expect(() => validateMeta(fileMeta('', ''))).toThrow(MetaError);
  });

  it('bounds file size to a non-negative integer up to the limit', () => {
    expect(() => validateMeta(sizedMeta(0))).not.toThrow();
    expect(() => validateMeta(sizedMeta(MAX_FILE_BYTES))).not.toThrow();
    expect(() => validateMeta(sizedMeta(MAX_FILE_BYTES + 1))).toThrow(MetaError);
    expect(() => validateMeta(sizedMeta(-1))).toThrow(MetaError);
    expect(() => validateMeta(sizedMeta(1.5))).toThrow(MetaError);
    expect(() => validateMeta(sizedMeta(NaN))).toThrow(MetaError);
  });

  it.each(['a/b', 'a\\b', 'a\0b', 'a\u{1F}b', 'a\u{7F}b', '/', '\\'])('forbids the file name %j', (name) => {
    expect(() => {
      validateMeta(fileMeta(name, ''));
    }).toThrow(MetaError);
  });

  it('allows spaces, dots, brackets and accents in file names', () => {
    expect(() => validateMeta({ file: { name: 'a b.čé (1).txt', mime: '', size: 1 } })).not.toThrow();
  });

  it('requires text or file', () => {
    expect(() => validateMeta({})).toThrow(MetaError);
    expect(() => encodeMeta({ text: undefined, file: undefined })).toThrow(MetaError);
  });

  it('rejects strings with lone surrogates', () => {
    expect(() => encodeMeta({ text: 'a\u{D800}' })).toThrow(MetaError);
    expect(() => encodeMeta({ file: { name: '\u{DC00}', mime: '', size: 1 } })).toThrow(MetaError);
    expect(() => encodeMeta({ file: { name: 'a', mime: '\u{D800}', size: 1 } })).toThrow(MetaError);
  });

  it('bounds the encoded meta even when the text fits', () => {
    expect(() => encodeMeta({ text: '"'.repeat(60_000) })).toThrow(MetaError);
    expect(encodeMeta({ text: 'x'.repeat(MAX_TEXT_BYTES) })).toHaveLength(MAX_TEXT_BYTES + 17);
  });

  it('bounds the decoded input', () => {
    const overhead = padded(0).length;
    expect(() => decodeText(padded(MAX_META_BYTES - overhead))).not.toThrow();
    expect(() => decodeText(padded(MAX_META_BYTES - overhead + 1))).toThrow(MetaError);
  });
});

describe('decoding', () => {
  it('accepts whitespace, any key order, unknown keys and duplicate keys', () => {
    expect(decodeText(' \n{ "text" : "x" ,\r\n\t"v" : 1 }\n ')).toEqual({ text: 'x' });
    expect(decodeText('{"v":1,"text":"x","extra":{"nested":[1,2.5e3,-0,true,false,null,"s",{}],"o":{}}}')).toEqual({
      text: 'x',
    });
    expect(decodeText('{"v":1,"text":"first","text":"second"}')).toEqual({ text: 'second' });
    expect(decodeText('{"v":2,"text":"x","v":1}')).toEqual({ text: 'x' });
  });

  it('treats null as an absent key', () => {
    expect(decodeText('{"v":1,"text":null,"file":{"name":"a","size":1}}')).toEqual({
      file: { name: 'a', mime: '', size: 1 },
    });
    expect(decodeText('{"v":1,"text":"x","file":null}')).toEqual({ text: 'x' });
    expect(decodeText('{"v":1,"file":{"name":"a","mime":null,"size":1}}')).toEqual({
      file: { name: 'a', mime: '', size: 1 },
    });
    expect(() => decodeText('{"v":1,"text":null}')).toThrow(MetaError);
    expect(() => decodeText('{"v":null,"text":"x"}')).toThrow(MetaError);
    expect(() => decodeText('{"v":1,"file":{"name":null,"size":1}}')).toThrow(MetaError);
    expect(() => decodeText('{"v":1,"file":{"name":"a","size":null}}')).toThrow(MetaError);
  });

  it('decodes JSON escapes including surrogate pairs', () => {
    expect(decodeText('{"v":1,"text":"a\\/b\\u00e9\\u00E9\\ud83d\\ude00\\"\\\\\\b\\f\\n\\r\\t"}')).toEqual({
      text: 'a/béé😀"\\\b\f\n\r\t',
    });
  });

  it('replaces an unpaired surrogate escape with U+FFFD like the Go core', () => {
    expect(decodeText('{"v":1,"text":"\\ud800x"}')).toEqual({ text: '�x' });
    expect(decodeText('{"v":1,"text":"\\udc00"}')).toEqual({ text: '�' });
    expect(decodeText('{"v":1,"text":"\\ud800\\ud800"}')).toEqual({ text: '��' });
    expect(decodeText('{"v":1,"text":"\\ud800\\u0041"}')).toEqual({ text: '�A' });
  });

  it.each([
    '',
    ' ',
    '[1]',
    '"x"',
    '1',
    'null',
    '{"v":1,"text":"x"} {}',
    '{"v":1,"text":"x"}x',
    '\u{FEFF}{"v":1,"text":"x"}',
    '{"v":1,"text":"x",}',
    '{"v":1,"text":"x"',
    '{"v":1,"text":"x\n"}',
    '{"v":1,"text":"x\\x"}',
    '{"v":1,"text":"\\u12"}',
    '{"v":1,"text":"\\u12G4"}',
    '{"v":1,"text":\'x\'}',
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

  it('rejects invalid UTF-8', () => {
    expect(() => decodeMeta(concatenate(utf8('{"v":1,"text":"'), Uint8Array.of(255), utf8('"}')))).toThrow(MetaError);
    expect(() => decodeMeta(Uint8Array.of(195))).toThrow(MetaError);
  });

  it.each([
    '{"text":"x"}',
    '{"v":"1","text":"x"}',
    '{"v":1.0,"text":"x"}',
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
    '{"v":1,"file":{"size":1}}',
    '{"v":1,"file":{"name":1,"size":1}}',
    '{"v":1,"file":{"name":"a","mime":1,"size":1}}',
    '{"v":1,"file":{"name":"a","size":1.0}}',
    '{"v":1,"file":{"name":"a","size":1e0}}',
    '{"v":1,"file":{"name":"a","size":"1"}}',
    '{"v":1,"file":{"name":"a","size":true}}',
    '{"v":1,"file":{"name":"a","size":99999999999999999999}}',
  ])('rejects the wrong type or non-integer in %s', (json) => {
    expect(() => decodeText(json)).toThrow(MetaError);
  });

  it('accepts -0 as size 0', () => {
    const meta = decodeText('{"v":1,"file":{"name":"a","size":-0}}');
    expect(meta.file?.size).toBe(0);
    expect(canonical(meta)).toBe('{"v":1,"file":{"name":"a","mime":"","size":0}}');
  });
});
