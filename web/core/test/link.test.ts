import { describe, expect, it } from 'vitest';

import {
  EncodingError,
  KDF_NONE,
  KDF_PBKDF2,
  KDF_SCRYPT,
  KdfError,
  KdfMismatchError,
  LinkError,
  assertProfile,
  decode32,
  decodeBase64urlExact,
  encodeBase64url,
  formatLink,
  fragment,
  idToString,
  newId,
  newSecret,
  parseFragment,
  parseId,
  parseKdfId,
  parseLink,
  parsePath,
} from '../src/index.ts';

const base = 'https://nylzo.example';

describe('AK24: the kdf profile comes from the fragment only', () => {
  it('assertProfile rejects a server-reported profile that differs from the fragment', () => {
    expect(() => assertProfile(KDF_SCRYPT, KDF_PBKDF2)).toThrow(KdfMismatchError);
    expect(() => assertProfile(KDF_SCRYPT, 3)).toThrow(KdfMismatchError);
    expect(() => assertProfile(KDF_SCRYPT, 257)).toThrow(KdfMismatchError);
    expect(() => assertProfile(KDF_SCRYPT, KDF_SCRYPT)).not.toThrow();
    expect(() => assertProfile(KDF_NONE, KDF_NONE)).not.toThrow();
  });

  it('a parsed link exposes the fragment profile and nothing else', () => {
    const id = newId();
    const s = newSecret();
    const link = parseLink(formatLink(base, id, KDF_SCRYPT, s));
    expect(link.kdfId).toBe(KDF_SCRYPT);
    expect(Object.keys(link).sort()).toEqual(['id', 'kdfId', 's']);
    expect(link.id).toEqual(id);
    expect(link.s).toEqual(s);
    expect(() => assertProfile(link.kdfId, KDF_PBKDF2)).toThrow(KdfMismatchError);
  });
});

describe('fragment', () => {
  const s = newSecret();
  const encoded = encodeBase64url(s);

  it('formats as 1.{kdf}.{S}', () => {
    expect(fragment(KDF_PBKDF2, s)).toBe(`1.2.${encoded}`);
    expect(encoded).toHaveLength(43);
  });

  it('parses with and without the leading hash sign', () => {
    for (const text of [`1.1.${encoded}`, `#1.1.${encoded}`]) {
      const parsed = parseFragment(text);
      expect(parsed.kdfId).toBe(KDF_SCRYPT);
      expect(parsed.s).toEqual(s);
    }
  });

  it('rejects unknown versions, profiles and non-canonical secrets', () => {
    const nonCanonicalLast = encoded.slice(0, -1) + (encoded.endsWith('B') ? 'C' : 'B');
    const bad = [
      '',
      '1',
      '1.1',
      `1.1.${encoded}.x`,
      `2.1.${encoded}`,
      `0.1.${encoded}`,
      `1.3.${encoded}`,
      `1.01.${encoded}`,
      `1..${encoded}`,
      `1.1.${encoded.slice(0, 42)}`,
      `1.1.${encoded}A`,
      `1.1.${encoded}=`,
      `1.1.${nonCanonicalLast}`,
      `1.1.${encoded.slice(0, 42)}+`,
      `1.1.${encoded.slice(0, 42)}/`,
      `##1.1.${encoded}`,
    ];
    for (const text of bad) {
      expect(() => parseFragment(text), text).toThrow(LinkError);
    }
  });
});

describe('links', () => {
  it('formats with a single /m/ path segment and trims one trailing slash', () => {
    const id = newId();
    const s = newSecret();
    const expected = `${base}/m/${idToString(id)}#${fragment(KDF_NONE, s)}`;
    expect(formatLink(base, id, KDF_NONE, s)).toBe(expected);
    expect(formatLink(`${base}/`, id, KDF_NONE, s)).toBe(expected);
  });

  it('rejects links that are not /m/{id}#fragment', () => {
    const id = idToString(newId());
    const frag = fragment(KDF_NONE, newSecret());
    const bad = [
      'not a url',
      `${base}/m/${id}`,
      `${base}/m/${id}#`,
      `${base}/x/${id}#${frag}`,
      `${base}/m/${id}/#${frag}`,
      `${base}/m/#${frag}`,
      `${base}/${id}#${frag}`,
      `${base}/m/${id.slice(0, 21)}#${frag}`,
    ];
    for (const raw of bad) {
      expect(() => parseLink(raw), raw).toThrow(LinkError);
    }
    expect(() => parsePath(`/x/${id}`)).toThrow(LinkError);
    expect(() => parsePath(`/m/${id}x`)).toThrow(LinkError);
  });
});

describe('canonical base64url', () => {
  it('ids are 22 characters ending in A, Q, g or w', () => {
    for (let i = 0; i < 200; i++) {
      const text = idToString(newId());
      expect(text).toHaveLength(22);
      expect('AQgw').toContain(text.slice(-1));
      expect(() => parseId(text)).not.toThrow();
    }
  });

  it('secrets are 43 characters never ending in - or _', () => {
    for (let i = 0; i < 200; i++) {
      const text = encodeBase64url(newSecret());
      expect(text).toHaveLength(43);
      expect('AEIMQUYcgkosw048').toContain(text.slice(-1));
      expect(() => decode32(text)).not.toThrow();
    }
  });

  it('rejects non-canonical ids', () => {
    const text = idToString(newId());
    const bad = [
      text.slice(0, 21) + 'B',
      `${text}=`,
      text.slice(0, 21),
      `${text}A`,
      `+${text.slice(1)}`,
      `/${text.slice(1)}`,
      `${text.slice(0, 21)}\n`,
      `${text.slice(0, 10)} ${text.slice(11)}`,
      '',
    ];
    for (const candidate of bad) {
      expect(() => parseId(candidate), JSON.stringify(candidate)).toThrow(EncodingError);
    }
  });

  it('round trips every length and rejects stray low bits', () => {
    for (let length = 0; length <= 64; length++) {
      const bytes = crypto.getRandomValues(new Uint8Array(length));
      const text = encodeBase64url(bytes);
      expect(decodeBase64urlExact(text, length)).toEqual(bytes);
    }
    expect(encodeBase64url(new Uint8Array([0xfb, 0xff]))).toBe('-_8');
    expect(Array.from(decodeBase64urlExact('-_8', 2))).toEqual([0xfb, 0xff]);
    expect(() => decodeBase64urlExact('-_9', 2)).toThrow(EncodingError);
    expect(() => decodeBase64urlExact('AB', 1)).toThrow(EncodingError);
  });
});

describe('kdf identifiers', () => {
  it('parses only 0, 1 and 2', () => {
    expect(parseKdfId('0')).toBe(KDF_NONE);
    expect(parseKdfId('1')).toBe(KDF_SCRYPT);
    expect(parseKdfId('2')).toBe(KDF_PBKDF2);
    for (const text of ['3', '01', '', ' 1', '1 ', '-1', '1.0']) {
      expect(() => parseKdfId(text), text).toThrow(KdfError);
    }
  });
});
