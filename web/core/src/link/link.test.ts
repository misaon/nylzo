import { describe, expect, it } from 'vitest';

import { randomId, thrownBy } from '../../test/vectors.ts';
import {
  assertProfile,
  decodeBase64urlExact,
  encodeBase64url,
  EncodingError,
  formatLink,
  fragment,
  idToString,
  KDF_NONE,
  KDF_RETIRED,
  KDF_SCRYPT,
  KdfError,
  KdfMismatchError,
  LinkError,
  LinkRetiredKdfError,
  LinkVersionError,
  newSecret,
  parseFragment,
  parseId,
  parseKdfId,
  parseLink,
  parsePath,
} from '../index.ts';

const base = 'https://nylzo.example';

describe('AK24: the kdf profile comes from the fragment only', () => {
  it('assertProfile rejects a server-reported profile that differs from the fragment', () => {
    expect(() => assertProfile(KDF_SCRYPT, KDF_RETIRED)).toThrow(KdfMismatchError);
    expect(() => assertProfile(KDF_SCRYPT, 3)).toThrow(KdfMismatchError);
    expect(() => assertProfile(KDF_SCRYPT, 257)).toThrow(KdfMismatchError);
    expect(() => assertProfile(KDF_SCRYPT, KDF_SCRYPT)).not.toThrow();
    expect(() => assertProfile(KDF_NONE, KDF_NONE)).not.toThrow();
  });

  it('a parsed link exposes the fragment profile and nothing else', () => {
    const id = randomId();
    const s = newSecret();
    const link = parseLink(formatLink(base, id, KDF_SCRYPT, s));
    expect(link.kdfId).toBe(KDF_SCRYPT);
    expect(Object.keys(link).toSorted((left, right) => left.localeCompare(right))).toEqual(['id', 'kdfId', 's']);
    expect(link.id).toEqual(id);
    expect(link.s).toEqual(s);
    expect(() => assertProfile(link.kdfId, KDF_NONE)).toThrow(KdfMismatchError);
  });
});

describe('fragment', () => {
  const s = newSecret();
  const encoded = encodeBase64url(s);

  it('formats as 1.{kdf}.{S}', () => {
    expect(fragment(KDF_SCRYPT, s)).toBe(`1.1.${encoded}`);
    expect(encoded).toHaveLength(43);
  });

  it('parses with and without the leading hash sign', () => {
    for (const text of [`1.1.${encoded}`, `#1.1.${encoded}`]) {
      const parsed = parseFragment(text);
      expect(parsed.kdfId).toBe(KDF_SCRYPT);
      expect(parsed.s).toEqual(s);
    }
  });

  const nonCanonicalLast = encoded.slice(0, -1) + (encoded.endsWith('B') ? 'C' : 'B');
  const badFragments = [
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

  it.each(badFragments)('rejects %j', (text) => {
    expect(() => parseFragment(text)).toThrow(LinkError);
  });
});

describe('links', () => {
  it('formats with a single /m/ path segment and trims one trailing slash', () => {
    const id = randomId();
    const s = newSecret();
    const expected = `${base}/m/${idToString(id)}#${fragment(KDF_NONE, s)}`;
    expect(formatLink(base, id, KDF_NONE, s)).toBe(expected);
    expect(formatLink(`${base}/`, id, KDF_NONE, s)).toBe(expected);
  });

  const id = idToString(randomId());
  const frag = fragment(KDF_NONE, newSecret());
  const badLinks = [
    'not a url',
    `${base}/m/${id}`,
    `${base}/m/${id}#`,
    `${base}/x/${id}#${frag}`,
    `${base}/m/${id}/#${frag}`,
    `${base}/m/#${frag}`,
    `${base}/${id}#${frag}`,
    `${base}/m/${id.slice(0, 21)}#${frag}`,
  ];

  it.each(badLinks)('rejects %j', (raw) => {
    expect(() => parseLink(raw)).toThrow(LinkError);
  });

  it('rejects paths that are not /m/{id}', () => {
    expect(() => parsePath(`/x/${id}`)).toThrow(LinkError);
    expect(() => parsePath(`/m/${id}x`)).toThrow(LinkError);
  });
});

describe('canonical base64url', () => {
  it('ids are 22 characters ending in A, Q, g or w', () => {
    for (let index = 0; index < 200; index++) {
      const text = idToString(randomId());
      expect(text).toHaveLength(22);
      expect('AQgw').toContain(text.slice(-1));
      expect(() => parseId(text)).not.toThrow();
    }
  });

  it('secrets are 43 characters never ending in - or _', () => {
    for (let index = 0; index < 200; index++) {
      const text = encodeBase64url(newSecret());
      expect(text).toHaveLength(43);
      expect('AEIMQUYcgkosw048').toContain(text.slice(-1));
      expect(() => decodeBase64urlExact(text, 32)).not.toThrow();
    }
  });

  const text = idToString(randomId());
  const badIds = [
    `${text.slice(0, 21)}B`,
    `${text}=`,
    text.slice(0, 21),
    `${text}A`,
    `+${text.slice(1)}`,
    `/${text.slice(1)}`,
    `${text.slice(0, 21)}\n`,
    `${text.slice(0, 10)} ${text.slice(11)}`,
    '',
  ];

  it.each(badIds)('rejects the non-canonical id %j', (candidate) => {
    expect(() => parseId(candidate)).toThrow(EncodingError);
  });

  it('round trips every length and rejects stray low bits', () => {
    for (let length = 0; length <= 64; length++) {
      const bytes = crypto.getRandomValues(new Uint8Array(length));
      const text = encodeBase64url(bytes);
      expect(decodeBase64urlExact(text, length)).toEqual(bytes);
    }
    expect(encodeBase64url(new Uint8Array([251, 255]))).toBe('-_8');
    expect([...decodeBase64urlExact('-_8', 2)]).toEqual([251, 255]);
    expect(() => decodeBase64urlExact('-_9', 2)).toThrow(EncodingError);
    expect(() => decodeBase64urlExact('AB', 1)).toThrow(EncodingError);
  });
});

describe('kdf identifiers', () => {
  it('parses 0 and 1', () => {
    expect(parseKdfId('0')).toBe(KDF_NONE);
    expect(parseKdfId('1')).toBe(KDF_SCRYPT);
  });

  it.each(['2', '3', '01', '', ' 1', '1 ', '-1', '1.0'])('rejects %j', (text) => {
    expect(() => parseKdfId(text)).toThrow(KdfError);
  });
});

describe('message ids', () => {
  it('the public API offers no way to mint a message id, the server issues them', async () => {
    const api: Record<string, unknown> = await import('../index.ts');
    expect(api['newId']).toBeUndefined();
    expect(typeof api['parseId']).toBe('function');
  });
});

describe('a newer fragment version', () => {
  const s = encodeBase64url(new Uint8Array(32));
  const cases: readonly (readonly [string, boolean])[] = [
    [`#2.1.${s}`, true],
    ['2', true],
    ['#7.a.b.c', true],
    [`#1.9.${s}`, false],
    [`#0.1.${s}`, false],
    ['#02.1.', false],
    ['#-2.1.', false],
    ['', false],
  ];
  for (const [text, newer] of cases) {
    it(`parseFragment(${JSON.stringify(text)}) tells a newer version apart: ${String(newer)}`, () => {
      const error = thrownBy(() => parseFragment(text));
      expect(error).toBeInstanceOf(LinkError);
      expect(error instanceof LinkVersionError).toBe(newer);
    });
  }
});

describe('the retired passphrase profile 2', () => {
  const s = encodeBase64url(new Uint8Array(32));
  const cases: readonly (readonly [string, boolean])[] = [
    [`#1.2.${s}`, true],
    [`1.2.${s}`, true],
    [`#1.2.${s.slice(0, 42)}`, false],
    [`#1.2.${s}A`, false],
    [`#1.02.${s}`, false],
    ['#1.2', false],
    [`#1.2.${s}.x`, false],
  ];
  for (const [text, retired] of cases) {
    it(`parseFragment(${JSON.stringify(text)}) tells the retired profile apart: ${String(retired)}`, () => {
      const error = thrownBy(() => parseFragment(text));
      expect(error).toBeInstanceOf(LinkError);
      expect(error instanceof LinkRetiredKdfError).toBe(retired);
    });
  }

  it('parseLink reports it only for an otherwise valid link', () => {
    const id = idToString(randomId());
    expect(thrownBy(() => parseLink(`${base}/m/${id}#1.2.${s}`))).toBeInstanceOf(LinkRetiredKdfError);
    const badPath = thrownBy(() => parseLink(`${base}/x/${id}#1.2.${s}`));
    expect(badPath).toBeInstanceOf(LinkError);
    expect(badPath).not.toBeInstanceOf(LinkRetiredKdfError);
  });
});
