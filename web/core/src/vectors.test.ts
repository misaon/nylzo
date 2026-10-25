import { scryptAsync } from '@noble/hashes/scrypt.js';
import { describe, expect, it } from 'vitest';

import { bytesToHex, fileBytes, hexToBytes, loadVectors, sha256Hex, thrownBy } from '../test/vectors.ts';
import { open, seal } from './envelope/envelope.ts';
import { aad, type Header, type Policy } from './envelope/header.ts';
import { decodeMeta, encodeMeta, type Meta } from './envelope/meta.ts';
import { LinkError, LinkRetiredKdfError, LinkVersionError, MetaError, WeakPassphraseError } from './errors.ts';
import { deriveContent, deriveLink, hsalt } from './keys/derive.ts';
import { assertStrongPassphrase, isKdfId, KDF_NONE, type KdfId } from './keys/kdf.ts';
import { HEADER_LEN, TAG_LEN, VERSION } from './limits.ts';
import { formatLink, fragment, parseLink } from './link/link.ts';
import { encodeBase64url } from './primitives/base64url.ts';
import { utf8 } from './primitives/bytes.ts';
import { idToString, parseId } from './primitives/identifiers.ts';
import {
  authorizationHeader,
  parseChallenge,
  PURPOSE_VERIFIER,
  type RequestKey,
  requestKey,
  requestMessage,
  seedRequestKey,
  signRequest,
  verifierHeader,
} from './request/signature.ts';
import { anyPassphraseKey, deriveAll } from './scrypt.ts';

const vectors = loadVectors();
const slow = { timeout: 120_000 };
const heavyEnabled = process.env['NYLZO_HEAVY_TESTS'] === '1';

function kdfIdOf(value: number): KdfId {
  if (!isKdfId(value)) {
    throw new Error(`vector uses unknown kdf_id ${value}`);
  }
  return value;
}

async function publicKeyOf(seed: Uint8Array): Promise<string> {
  const key = await requestKey(seed);
  return encodeBase64url(key.publicKey);
}

describe('spec/vectors.json', () => {
  it('targets protocol v1 and has cases in every section', () => {
    expect(vectors.spec).toBe('nylzo/v1');
    expect(vectors.scrypt_rfc7914).toHaveLength(4);
    const sections = [
      vectors.derive,
      vectors.envelope,
      vectors.link,
      vectors.meta,
      vectors.passphrase_strength,
      vectors.request,
      vectors.pow,
    ];
    expect(sections.map((section) => section.length)).not.toContain(0);
    expect(vectors.link.some((vector) => vector.canonical === true)).toBe(true);
  });
});

describe('scrypt_rfc7914', () => {
  const light = vectors.scrypt_rfc7914.filter((vector) => !vector.heavy);
  const heavy = vectors.scrypt_rfc7914.filter((vector) => vector.heavy);
  const derivedHex = async (vector: (typeof light)[number]): Promise<string> => {
    const parameters = { N: vector.N, r: vector.r, p: vector.p, dkLen: vector.dkLen };
    return bytesToHex(await scryptAsync(utf8(vector.password), utf8(vector.salt), parameters));
  };

  it.each(light)('$name', async (vector) => {
    expect(await derivedHex(vector)).toBe(vector.dk_hex);
  });

  it.runIf(heavyEnabled).each(heavy)('$name', { timeout: 600_000 }, async (vector) => {
    expect(await derivedHex(vector)).toBe(vector.dk_hex);
  });
});

describe('derive', () => {
  it.each(vectors.derive)('$name', slow, async (vector) => {
    const id = parseId(vector.id);
    const secret = hexToBytes(vector.s);
    const kdfId = kdfIdOf(vector.kdf_id);
    expect(bytesToHex(await hsalt(id))).toBe(vector.hsalt_hex);

    const link = await deriveLink(id, secret);
    expect(bytesToHex(link.r)).toBe(vector.r_hex);
    expect(bytesToHex(link.saltPw)).toBe(vector.salt_pw_hex);

    const passphraseKey = await anyPassphraseKey(kdfId, vector.passphrase ?? '', link.saltPw);
    expect(bytesToHex(passphraseKey)).toBe(vector.p_hex);

    const content = await deriveContent(id, secret, passphraseKey);
    expect(bytesToHex(content.v)).toBe(vector.v_hex);
    expect(content.k.extractable).toBe(false);

    expect(await publicKeyOf(link.r)).toBe(vector.read_key);
    expect(await publicKeyOf(hexToBytes(vector.m_hex))).toBe(vector.manage_key);
    expect(kdfId === KDF_NONE ? undefined : await publicKeyOf(content.v)).toBe(vector.verifier_key);
    expect(fragment(kdfId, secret)).toBe(vector.fragment);

    const all = await deriveAll(id, secret, kdfId, vector.passphrase ?? '');
    expect(bytesToHex(all.link.r)).toBe(vector.r_hex);
    expect(bytesToHex(all.content.v)).toBe(vector.v_hex);
  });

  it('NFC composed and decomposed passphrases agree', () => {
    const composed = vectors.derive.find((vector) => vector.name === 'scrypt-nfc-composed');
    const decomposed = vectors.derive.find((vector) => vector.name === 'scrypt-nfc-decomposed');
    expect(composed).toBeDefined();
    expect(composed?.passphrase).not.toBe(decomposed?.passphrase);
    expect(composed?.p_hex).toBe(decomposed?.p_hex);
    expect(composed?.k_hex).toBe(decomposed?.k_hex);
  });
});

describe('envelope', () => {
  it.each(vectors.envelope)('$name', slow, async (vector) => {
    const id = parseId(vector.id);
    const kdfId = kdfIdOf(vector.kdf_id);
    const { content } = await deriveAll(id, hexToBytes(vector.s), kdfId, vector.passphrase ?? '');
    const header: Header = { version: VERSION, kdfId, flags: vector.flags, iv: hexToBytes(vector.iv_hex) };
    const policy: Policy = { opensMax: vector.opens_max, ttlSeconds: vector.ttl_s };
    const file = vector.file && fileBytes(vector.file);
    const fileMeta = vector.file && { name: vector.file.name, mime: vector.file.mime, size: vector.file.size };
    const meta: Meta = { text: vector.text, file: fileMeta };

    expect(new TextDecoder().decode(encodeMeta(meta))).toBe(vector.meta_json);
    expect(bytesToHex(aad(header, id, policy))).toBe(vector.aad_hex);

    const object = await seal(content.k, header, id, policy, { meta, file });
    expect(object.length).toBe(vector.object_len);
    expect(object.length - HEADER_LEN - TAG_LEN).toBe(vector.plaintext_len);
    expect(await sha256Hex(object)).toBe(vector.object_sha256);
    expect(vector.object_b64 === undefined ? undefined : encodeBase64url(object)).toBe(vector.object_b64);

    const opened = await open(content.k, id, policy, kdfId, object);
    expect(opened.header).toEqual(header);
    expect(opened.payload.meta).toEqual(meta);
    expect(opened.payload.file).toEqual(file);
  });
});

describe('link', () => {
  const valid = vectors.link.filter((vector) => vector.valid);
  const canonical = valid.filter((vector) => vector.canonical === true);
  const invalid = vectors.link.filter((vector) => !vector.valid);

  it.each(valid)('$name', (vector) => {
    const link = parseLink(vector.input);
    expect(idToString(link.id)).toBe(vector.id);
    expect(link.kdfId).toBe(vector.kdf_id);
    expect(bytesToHex(link.s)).toBe(vector.s_hex);
  });

  it.each(canonical)('$name formats back to its input', (vector) => {
    const link = parseLink(vector.input);
    expect(formatLink('https://nylzo.example', link.id, link.kdfId, link.s)).toBe(vector.input);
  });

  it.each(invalid)('$name is rejected', (vector) => {
    const error = thrownBy(() => parseLink(vector.input));
    expect(error).toBeInstanceOf(LinkError);
    expect(error instanceof LinkVersionError).toBe(vector.newer_version === true);
    expect(error instanceof LinkRetiredKdfError).toBe(vector.retired_kdf === true);
  });
});

describe('meta', () => {
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

describe('passphrase_strength', () => {
  const accepted = vectors.passphrase_strength.filter((vector) => vector.accepted);
  const rejected = vectors.passphrase_strength.filter((vector) => !vector.accepted);

  it.each(accepted)('$name is accepted', (vector) => {
    expect(() => {
      assertStrongPassphrase(vector.passphrase);
    }).not.toThrow();
  });

  it.each(rejected)('$name is rejected', (vector) => {
    expect(() => {
      assertStrongPassphrase(vector.passphrase);
    }).toThrow(WeakPassphraseError);
  });
});

describe('request', () => {
  it.each(vectors.request)('$name', async (vector) => {
    const seed = hexToBytes(vector.seed_hex);
    const key = await requestKey(seed);
    const fallback = seedRequestKey(seed);
    expect('privateKey' in key && key.privateKey.extractable).toBe(false);
    expect(encodeBase64url(key.publicKey)).toBe(vector.public_key);
    expect(encodeBase64url(fallback.publicKey)).toBe(vector.public_key);

    const challenge = parseChallenge(vector.challenge);
    const body = hexToBytes(vector.body_hex);
    const message = await requestMessage(vector.purpose, vector.method, vector.path, challenge, body);
    expect(bytesToHex(message)).toBe(vector.message_hex);

    const sign = (signingKey: RequestKey) =>
      signRequest(signingKey, vector.purpose, vector.method, vector.path, challenge, body);
    const signature = await sign(key);
    const fallbackSignature = await sign(fallback);
    expect(encodeBase64url(signature.signature)).toBe(vector.signature);
    expect(encodeBase64url(fallbackSignature.signature)).toBe(vector.signature);
    expect({ authorization: vector.authorization, verifier_header: vector.verifier_header }).toEqual(
      vector.purpose === PURPOSE_VERIFIER
        ? { verifier_header: verifierHeader(signature) }
        : { authorization: authorizationHeader(signature) },
    );
  });
});
