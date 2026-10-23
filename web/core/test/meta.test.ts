import { describe, expect, it } from 'vitest';

import {
  MAX_FILE_BYTES,
  MAX_FILE_NAME,
  MAX_META_BYTES,
  MAX_MIME,
  MAX_TEXT_BYTES,
  MetaError,
  decodeMeta,
  encodeMeta,
  utf8,
  validateMeta,
  type Meta,
} from '../src/index.ts';

const decoder = new TextDecoder();

function canonical(meta: Meta): string {
  return decoder.decode(encodeMeta(meta));
}

function decodeText(json: string): Meta {
  return decodeMeta(utf8(json));
}

describe('canonical encoding', () => {
  it('applies the escaping table of the spec', () => {
    const text = 'q"b\\s\b\t\n\f\r\u0001\u001f/é\u2028\u2029\u007f😀';
    expect(canonical({ text })).toBe(
      '{"v":1,"text":"q\\"b\\\\s\\b\\t\\n\\f\\r\\u0001\\u001f/é\u2028\u2029\u007f😀"}',
    );
  });

  it('escapes every other C0 control as lowercase \\u00xx', () => {
    for (let code = 0; code < 0x20; code++) {
      const char = String.fromCharCode(code);
      const named = new Map([
        [0x08, '\\b'],
        [0x09, '\\t'],
        [0x0a, '\\n'],
        [0x0c, '\\f'],
        [0x0d, '\\r'],
      ]).get(code);
      const expected = named ?? `\\u00${code.toString(16).padStart(2, '0')}`;
      expect(canonical({ text: char })).toBe(`{"v":1,"text":"${expected}"}`);
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
      { text: '\u0000\u001f\u007f', file: { name: ' ', mime: 'x/y; q="1"', size: MAX_FILE_BYTES } },
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
    const file = (name: string, mime: string): Meta => ({ file: { name, mime, size: 1 } });
    expect(() => validateMeta(file('n'.repeat(MAX_FILE_NAME), ''))).not.toThrow();
    expect(() => validateMeta(file('n'.repeat(MAX_FILE_NAME + 1), ''))).toThrow(MetaError);
    expect(() => validateMeta(file('é'.repeat(127), ''))).not.toThrow();
    expect(() => validateMeta(file('é'.repeat(128), ''))).toThrow(MetaError);
    expect(() => validateMeta(file('a', 'm'.repeat(MAX_MIME)))).not.toThrow();
    expect(() => validateMeta(file('a', 'm'.repeat(MAX_MIME + 1)))).toThrow(MetaError);
    expect(() => validateMeta(file('', ''))).toThrow(MetaError);
  });

  it('bounds file size to a non-negative integer up to the limit', () => {
    const size = (value: number): Meta => ({ file: { name: 'a', mime: '', size: value } });
    expect(() => validateMeta(size(0))).not.toThrow();
    expect(() => validateMeta(size(MAX_FILE_BYTES))).not.toThrow();
    expect(() => validateMeta(size(MAX_FILE_BYTES + 1))).toThrow(MetaError);
    expect(() => validateMeta(size(-1))).toThrow(MetaError);
    expect(() => validateMeta(size(1.5))).toThrow(MetaError);
    expect(() => validateMeta(size(Number.NaN))).toThrow(MetaError);
  });

  it('forbids path separators and control characters in file names', () => {
    for (const name of ['a/b', 'a\\b', 'a\u0000b', 'a\u001fb', 'a\u007fb', '/', '\\']) {
      expect(() => validateMeta({ file: { name, mime: '', size: 1 } }), JSON.stringify(name)).toThrow(MetaError);
    }
    expect(() => validateMeta({ file: { name: 'a b.čé (1).txt', mime: '', size: 1 } })).not.toThrow();
  });

  it('requires text or file', () => {
    expect(() => validateMeta({})).toThrow(MetaError);
    expect(() => encodeMeta({ text: undefined, file: undefined })).toThrow(MetaError);
  });

  it('rejects strings with lone surrogates', () => {
    expect(() => encodeMeta({ text: 'a\ud800' })).toThrow(MetaError);
    expect(() => encodeMeta({ file: { name: '\udc00', mime: '', size: 1 } })).toThrow(MetaError);
    expect(() => encodeMeta({ file: { name: 'a', mime: '\ud800', size: 1 } })).toThrow(MetaError);
  });

  it('bounds the encoded meta even when the text fits', () => {
    expect(() => encodeMeta({ text: '"'.repeat(60000) })).toThrow(MetaError);
    expect(encodeMeta({ text: 'x'.repeat(MAX_TEXT_BYTES) })).toHaveLength(MAX_TEXT_BYTES + 17);
  });

  it('bounds the decoded input', () => {
    const pad = (n: number): string => `{"v":1,"text":"x","pad":"${'p'.repeat(n)}"}`;
    const overhead = pad(0).length;
    expect(() => decodeText(pad(MAX_META_BYTES - overhead))).not.toThrow();
    expect(() => decodeText(pad(MAX_META_BYTES - overhead + 1))).toThrow(MetaError);
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

  it('rejects malformed documents', () => {
    const bad = [
      '',
      ' ',
      '[1]',
      '"x"',
      '1',
      'null',
      '{"v":1,"text":"x"} {}',
      '{"v":1,"text":"x"}x',
      '\ufeff{"v":1,"text":"x"}',
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
    ];
    for (const json of bad) {
      expect(() => decodeText(json), JSON.stringify(json)).toThrow(MetaError);
    }
  });

  it('rejects invalid UTF-8', () => {
    expect(() => decodeMeta(new Uint8Array([0x7b, 0x22, 0x76, 0x22, 0x3a, 0x31, 0x2c, 0x22, 0x74, 0x65, 0x78, 0x74, 0x22, 0x3a, 0x22, 0xff, 0x22, 0x7d]))).toThrow(MetaError);
    expect(() => decodeMeta(new Uint8Array([0xc3]))).toThrow(MetaError);
  });

  it('rejects wrong types and non-integer numbers', () => {
    const bad = [
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
    ];
    for (const json of bad) {
      expect(() => decodeText(json), json).toThrow(MetaError);
    }
  });

  it('accepts -0 as size 0', () => {
    const meta = decodeText('{"v":1,"file":{"name":"a","size":-0}}');
    expect(meta.file?.size).toBe(0);
    expect(canonical(meta)).toBe('{"v":1,"file":{"name":"a","mime":"","size":0}}');
  });
});
