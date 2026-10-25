import { describe, expect, it } from 'vitest';

import { bytesToHex, randomId, thrownBy, vectors } from '../../test/vectors.ts';
import { KdfMismatchError, LinkError, LinkRetiredKdfError, LinkVersionError } from '../errors.ts';
import { KDF_NONE, KDF_RETIRED, KDF_SCRYPT, type KdfId } from '../keys/kdf.ts';
import { encodeBase64url } from '../primitives/base64url.ts';
import { idToString, newSecret } from '../primitives/identifiers.ts';
import { assertProfile, formatLink, fragment, parseFragment, parseLink } from './link.ts';

const base = 'https://nylzo.example';

describe('AK9: link vectors', () => {
  const valid = vectors.link.filter((vector) => vector.valid);
  const canonical = valid.filter((vector) => vector.canonical === true);
  const invalid = vectors.link.filter((vector) => !vector.valid);

  it.each(valid)('$name', (vector) => {
    const link = parseLink(vector.input);
    expect(idToString(link.id)).toBe(vector.id);
    expect(link.kdfId).toBe(vector.kdf_id);
    expect(bytesToHex(link.secret)).toBe(vector.s_hex);
  });

  it.each(canonical)('$name formats back to its input', (vector) => {
    const link = parseLink(vector.input);
    expect(formatLink(base, link.id, link.kdfId, link.secret)).toBe(vector.input);
  });

  it.each(invalid)('$name is rejected', (vector) => {
    const error = thrownBy(() => parseLink(vector.input));
    expect(error).toBeInstanceOf(LinkError);
    expect(error instanceof LinkVersionError).toBe(vector.newer_version === true);
    expect(error instanceof LinkRetiredKdfError).toBe(vector.retired_kdf === true);
  });
});

describe('AK24: the kdf profile comes from the fragment only', () => {
  it.each<[KdfId, number]>([
    [KDF_SCRYPT, KDF_NONE],
    [KDF_NONE, KDF_SCRYPT],
    [KDF_SCRYPT, KDF_RETIRED],
    [KDF_SCRYPT, 257],
  ])('rejects the server profile when the fragment says %i and the server %i', (fragmentKdfId, serverKdfId) => {
    expect(() => {
      assertProfile(fragmentKdfId, serverKdfId);
    }).toThrow(KdfMismatchError);
  });

  it('accepts the server profile that matches the fragment', () => {
    expect(() => {
      assertProfile(KDF_SCRYPT, KDF_SCRYPT);
    }).not.toThrow();
  });

  it('exposes nothing but the id and the fragment of a parsed link', () => {
    const id = randomId();
    const secret = newSecret();
    expect(parseLink(formatLink(base, id, KDF_SCRYPT, secret))).toStrictEqual({ id, kdfId: KDF_SCRYPT, secret });
  });
});

describe('links and fragments (spec 4)', () => {
  const secret = newSecret();
  const encoded = encodeBase64url(secret);

  it('formats a link under one trailing slash of the base', () => {
    const id = randomId();
    const expected = `${base}/m/${idToString(id)}#${fragment(KDF_NONE, secret)}`;
    expect(formatLink(base, id, KDF_NONE, secret)).toBe(expected);
    expect(formatLink(`${base}/`, id, KDF_NONE, secret)).toBe(expected);
  });

  it.each([`1.1.${encoded}`, `#1.1.${encoded}`])('parses the fragment %s', (text) => {
    expect(parseFragment(text)).toStrictEqual({ kdfId: KDF_SCRYPT, secret });
  });

  it.each<[string, new () => LinkError]>([
    ['', LinkError],
    [`##1.1.${encoded}`, LinkError],
    ['2', LinkVersionError],
    [`2.1.${encoded}`, LinkVersionError],
    [`#1.2.${encoded}`, LinkRetiredKdfError],
    [`#1.2.${encoded.slice(0, 42)}`, LinkError],
  ])('rejects the fragment %j', (text, expected) => {
    expect(thrownBy(() => parseFragment(text))).toStrictEqual(new expected());
  });

  it('offers no way to mint a message id, since the server issues them (spec 5.4)', async () => {
    const api: Record<string, unknown> = await import('../index.ts');
    expect(api['newId']).toBeUndefined();
    expect(typeof api['parseId']).toBe('function');
  });
});
