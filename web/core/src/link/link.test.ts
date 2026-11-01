import { describe, expect, it } from 'vitest';

import { bytesToHex, randomId, thrownBy, vectors } from '../../test/vectors.ts';
import { KdfMismatchError, LinkError, LinkReleasedError, LinkRetiredKdfError, LinkVersionError } from '../errors.ts';
import { KDF_NONE, KDF_RETIRED, KDF_SCRYPT, type KdfId } from '../keys/kdf.ts';
import { encodeBase64url } from '../primitives/base64url.ts';
import { idToString, newSecret } from '../primitives/identifiers.ts';
import { assertProfile, formatLink, parseFragment, parseLink } from './link.ts';

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
    expect(bytesToHex(link.objectHash)).toBe(vector.h_hex);
  });

  it.each(canonical)('$name formats back to its input', (vector) => {
    const link = parseLink(vector.input);
    expect(formatLink(base, link.id, link.kdfId, link.secret, link.objectHash)).toBe(vector.input);
  });

  it.each(invalid)('$name is rejected', (vector) => {
    const error = thrownBy(() => parseLink(vector.input));
    expect(error).toBeInstanceOf(LinkError);
    expect(error instanceof LinkVersionError).toBe(vector.newer_version === true);
    expect(error instanceof LinkRetiredKdfError).toBe(vector.retired_kdf === true);
    expect(error instanceof LinkReleasedError).toBe(vector.released === true);
  });
});

describe('AK24: the kdf profile comes from the fragment only', () => {
  it.each<[KdfId, number]>([
    [KDF_SCRYPT, KDF_NONE],
    [KDF_NONE, KDF_SCRYPT],
    [KDF_SCRYPT, KDF_RETIRED],
    [KDF_SCRYPT, 1],
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

  it('exposes nothing but the fields of the fragment', () => {
    const id = randomId();
    const secret = newSecret();
    const objectHash = newSecret();
    expect(parseLink(formatLink(base, id, KDF_SCRYPT, secret, objectHash))).toStrictEqual({
      kdfId: KDF_SCRYPT,
      id,
      secret,
      objectHash,
    });
  });
});

describe('links and fragments (spec 4, 15)', () => {
  const secret = newSecret();
  const encoded = encodeBase64url(secret);
  const id = randomId();
  const fields = `${idToString(id)}.${encoded}.${encoded}`;

  it('formats a link under one trailing slash of the base, with no id in the path', () => {
    const expected = `${base}/r#2.0.${fields}`;
    expect(formatLink(base, id, KDF_NONE, secret, secret)).toBe(expected);
    expect(formatLink(`${base}/`, id, KDF_NONE, secret, secret)).toBe(expected);
  });

  it.each([`2.3.${fields}`, `#2.3.${fields}`])('parses the fragment %s', (text) => {
    expect(parseFragment('/r', text)).toStrictEqual({ kdfId: KDF_SCRYPT, id, secret, objectHash: secret });
  });

  it('tells a link of protocol version 1 apart only on its own page, and reads nothing of it', () => {
    expect(thrownBy(() => parseFragment(`/m/${idToString(id)}`, `1.1.${encoded}`))).toStrictEqual(
      new LinkReleasedError(),
    );
    expect(thrownBy(() => parseFragment('/r', `1.1.${encoded}`))).toStrictEqual(new LinkError());
    expect(thrownBy(() => parseFragment(`/m/${idToString(id)}`, `2.3.${fields}`))).toStrictEqual(new LinkError());
  });

  it.each<[string, new () => LinkError]>([
    ['', LinkError],
    [`##2.3.${fields}`, LinkError],
    ['3', LinkVersionError],
    [`3.3.${fields}`, LinkVersionError],
    [`#2.2.${fields}`, LinkRetiredKdfError],
    [`#1.2.${encoded}`, LinkError],
    [`#2.1.${fields}`, LinkError],
    [`#1.3.${encoded}`, LinkError],
    [`#2.2.${fields.slice(0, -1)}`, LinkError],
  ])('rejects the fragment %j', (text, expected) => {
    expect(thrownBy(() => parseFragment('/r', text))).toStrictEqual(new expected());
  });

  it('offers no way to mint a message id, since the server issues them (spec 5.4)', async () => {
    const api: Record<string, unknown> = await import('../index.ts');
    expect(api['newId']).toBeUndefined();
    expect(typeof api['parseId']).toBe('function');
  });
});
