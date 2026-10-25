import { beforeAll, describe, expect, it, vi } from 'vitest';

import { bytesToHex, fileBytes, hexToBytes, kdfIdOf, randomId, sha256Hex, vectors } from '../../test/vectors.ts';
import {
  FlagsError,
  HeaderError,
  IntegrityError,
  KdfError,
  KdfMismatchError,
  MetaError,
  PolicyError,
  VersionError,
} from '../errors.ts';
import { deriveContent } from '../keys/derive.ts';
import { KDF_NONE, KDF_RETIRED, KDF_SCRYPT, type KdfId } from '../keys/kdf.ts';
import {
  HEADER_LENGTH,
  IV_LENGTH,
  MAX_FILE_BYTES,
  MAX_OBJECT_BYTES,
  MAX_TEXT_BYTES,
  TAG_LENGTH,
  VERSION,
} from '../limits.ts';
import { encodeBase64url } from '../primitives/base64url.ts';
import { newSecret, parseId } from '../primitives/identifiers.ts';
import { deriveAll } from '../scrypt.ts';
import { open, type Payload, seal } from './envelope.ts';
import { aad, type Header, newHeader, type Policy } from './header.ts';
import { encodeMeta, type Meta } from './meta.ts';

interface Sealed {
  readonly id: Uint8Array;
  readonly secret: Uint8Array;
  readonly contentKey: CryptoKey;
  readonly object: Uint8Array<ArrayBuffer>;
}

const policy: Policy = { opensMax: 1, ttlSeconds: 86_400 };
const textPayload: Payload = { meta: { text: 'integrity' } };
const passphraseKey = new Uint8Array(32).fill(1);

async function contentKeyOf(id: Uint8Array, secret: Uint8Array, key: Uint8Array): Promise<CryptoKey> {
  const { contentKey } = await deriveContent(id, secret, key);
  return contentKey;
}

async function sealed(kdfId: KdfId, payload = textPayload): Promise<Sealed> {
  const id = randomId();
  const secret = newSecret();
  const contentKey = await contentKeyOf(id, secret, kdfId === KDF_NONE ? new Uint8Array(0) : passphraseKey);
  const object = await seal(contentKey, newHeader(kdfId), id, policy, payload);
  return { id, secret, contentKey, object };
}

function flipped(bytes: Uint8Array, index: number, mask = 1): Uint8Array<ArrayBuffer> {
  const copy = Uint8Array.from(bytes);
  copy[index] = (copy[index] ?? 0) ^ mask;
  return copy;
}

async function digestOf(bytes: Uint8Array | undefined): Promise<string | undefined> {
  return bytes && (await sha256Hex(bytes));
}

describe('AK9: envelope vectors', () => {
  it.each(vectors.envelope)('$name', { timeout: 120_000 }, async (vector) => {
    const id = parseId(vector.id);
    const kdfId = kdfIdOf(vector.kdf_id);
    const { content } = await deriveAll(id, hexToBytes(vector.s), kdfId, vector.passphrase ?? '');
    const header: Header = { version: VERSION, kdfId, flags: vector.flags, iv: hexToBytes(vector.iv_hex) };
    const vectorPolicy: Policy = { opensMax: vector.opens_max, ttlSeconds: vector.ttl_s };
    const file = vector.file && fileBytes(vector.file);
    const fileMeta = vector.file && { name: vector.file.name, mime: vector.file.mime, size: vector.file.size };
    const meta: Meta = { text: vector.text, file: fileMeta };

    expect(new TextDecoder().decode(encodeMeta(meta))).toBe(vector.meta_json);
    expect(bytesToHex(aad(header, id, vectorPolicy))).toBe(vector.aad_hex);

    const object = await seal(content.contentKey, header, id, vectorPolicy, { meta, file });
    expect(object.length).toBe(vector.object_len);
    expect(object.length - HEADER_LENGTH - TAG_LENGTH).toBe(vector.plaintext_len);
    expect(await sha256Hex(object)).toBe(vector.object_sha256);
    expect(vector.object_b64 === undefined ? undefined : encodeBase64url(object)).toBe(vector.object_b64);

    const opened = await open(content.contentKey, id, vectorPolicy, kdfId, object);
    expect(opened.header).toEqual(header);
    expect(opened.payload.meta).toEqual(meta);
    expect(await digestOf(opened.payload.file)).toBe(await digestOf(file));
  });
});

describe('AK8: a changed envelope, policy or key fails integrity', () => {
  let message: Sealed;
  beforeAll(async () => {
    message = await sealed(KDF_SCRYPT);
  });

  it.each<[string, (sealedMessage: Sealed) => Promise<unknown>]>([
    [
      'a flipped ciphertext bit',
      ({ contentKey, id, object }) => open(contentKey, id, policy, KDF_SCRYPT, flipped(object, HEADER_LENGTH)),
    ],
    [
      'a flipped bit in the middle',
      ({ contentKey, id, object }) => open(contentKey, id, policy, KDF_SCRYPT, flipped(object, HEADER_LENGTH + 100)),
    ],
    [
      'a flipped tag bit',
      ({ contentKey, id, object }) => open(contentKey, id, policy, KDF_SCRYPT, flipped(object, object.length - 1)),
    ],
    ['a flipped iv bit', ({ contentKey, id, object }) => open(contentKey, id, policy, KDF_SCRYPT, flipped(object, 5))],
    [
      'a raised opens_max',
      ({ contentKey, id, object }) => open(contentKey, id, { ...policy, opensMax: 2 }, KDF_SCRYPT, object),
    ],
    [
      'a longer TTL',
      ({ contentKey, id, object }) =>
        open(contentKey, id, { ...policy, ttlSeconds: policy.ttlSeconds + 1 }, KDF_SCRYPT, object),
    ],
    ['another id', ({ contentKey, object }) => open(contentKey, randomId(), policy, KDF_SCRYPT, object)],
    [
      'another secret',
      async ({ id, object }) =>
        open(await contentKeyOf(id, newSecret(), passphraseKey), id, policy, KDF_SCRYPT, object),
    ],
    [
      'a wrong passphrase',
      async ({ id, secret, object }) =>
        open(await contentKeyOf(id, secret, new Uint8Array(32)), id, policy, KDF_SCRYPT, object),
    ],
  ])('%s', async (_, tamper) => {
    await expect(tamper(message)).rejects.toBeInstanceOf(IntegrityError);
  });
});

describe('open rejects a malformed header before decrypting (spec 3)', () => {
  let message: Sealed;
  beforeAll(async () => {
    message = await sealed(KDF_NONE);
  });

  it.each<[string, (object: Uint8Array) => Uint8Array, KdfId, new () => Error]>([
    ['version 2', (object) => flipped(object, 0, 3), KDF_NONE, VersionError],
    ['flags 1', (object) => flipped(object, 2), KDF_NONE, FlagsError],
    ['the retired profile 2', (object) => flipped(object, 1, KDF_RETIRED), KDF_RETIRED as KdfId, KdfError],
    ['a profile other than the fragment', (object) => object, KDF_SCRYPT, KdfMismatchError],
    ['an object without a whole tag', (object) => object.subarray(0, HEADER_LENGTH + 3), KDF_NONE, IntegrityError],
    ['a truncated header', (object) => object.subarray(0, HEADER_LENGTH - 1), KDF_NONE, HeaderError],
  ])('%s', async (_, change, kdfId, expected) => {
    await expect(open(message.contentKey, message.id, policy, kdfId, change(message.object))).rejects.toBeInstanceOf(
      expected,
    );
  });
});

describe('seal rejects what the spec forbids (spec 3 and 3.1)', () => {
  const header = newHeader(KDF_NONE);
  const oversizedFile = new Uint8Array(MAX_FILE_BYTES + 1);
  let contentKey: CryptoKey;
  beforeAll(async () => {
    contentKey = await contentKeyOf(randomId(), newSecret(), new Uint8Array(0));
  });

  it.each<[string, Header, Policy, Payload, new () => Error]>([
    [
      'a file over the limit',
      header,
      policy,
      { meta: { file: { name: 'x', mime: '', size: oversizedFile.length } }, file: oversizedFile },
      MetaError,
    ],
    ['text over the limit', header, policy, { meta: { text: 'x'.repeat(MAX_TEXT_BYTES + 1) } }, MetaError],
    [
      'a file shorter than meta.file.size',
      header,
      policy,
      { meta: { file: { name: 'x', mime: '', size: 2 } }, file: new Uint8Array(1) },
      MetaError,
    ],
    ['file bytes without meta.file', header, policy, { meta: { text: 't' }, file: new Uint8Array(1) }, MetaError],
    ['opens_max 0', header, { opensMax: 0, ttlSeconds: 60 }, textPayload, PolicyError],
    ['a TTL under a minute', header, { opensMax: 1, ttlSeconds: 59 }, textPayload, PolicyError],
    ['opens_max over 16 bits', header, { opensMax: 65_536, ttlSeconds: 60 }, textPayload, PolicyError],
    ['a fractional opens_max', header, { opensMax: 1.5, ttlSeconds: 60 }, textPayload, PolicyError],
    ['flags 1', { ...header, flags: 1 }, policy, textPayload, FlagsError],
    ['version 2', { ...header, version: 2 }, policy, textPayload, VersionError],
    ['the retired profile 2', { ...header, kdfId: KDF_RETIRED as KdfId }, policy, textPayload, KdfError],
    ['an 11-byte iv', { ...header, iv: new Uint8Array(11) }, policy, textPayload, HeaderError],
  ])('%s', async (_, invalidHeader, invalidPolicy, payload, expected) => {
    await expect(seal(contentKey, invalidHeader, randomId(), invalidPolicy, payload)).rejects.toBeInstanceOf(expected);
  });

  it('fits the largest message into MAX_OBJECT_BYTES', { timeout: 60_000 }, async () => {
    const file = new Uint8Array(MAX_FILE_BYTES).fill(7);
    const text = 'y'.repeat(MAX_TEXT_BYTES);
    const meta = { text, file: { name: 'max.bin', mime: 'application/octet-stream', size: file.length } };
    const message = await sealed(KDF_NONE, { meta, file });
    expect(message.object.length).toBeLessThanOrEqual(MAX_OBJECT_BYTES);
    const opened = await open(message.contentKey, message.id, policy, KDF_NONE, message.object);
    expect(opened.payload.meta.text).toBe(text);
    expect(await sha256Hex(opened.payload.file ?? new Uint8Array(0))).toBe(await sha256Hex(file));
  });
});

describe('secrets in the envelope', () => {
  it('get a fresh random iv for every message', () => {
    const seen = new Set<string>();
    const distinct = Array.from({ length: IV_LENGTH }, () => new Set<number>());
    for (let count = 0; count < 4096; count++) {
      const { iv } = newHeader(KDF_NONE);
      seen.add(bytesToHex(iv));
      for (const [index, byte] of iv.entries()) {
        distinct[index]?.add(byte);
      }
    }
    expect(seen.size).toBe(4096);
    expect(Math.min(...distinct.map((values) => values.size))).toBeGreaterThanOrEqual(200);
  });

  it('wipe the plaintext after seal and open, and the opened file is its own copy', async () => {
    const file = new Uint8Array(5000).fill(7);
    const payload: Payload = { meta: { file: { name: 'f.bin', mime: '', size: file.length } }, file };
    const message = await sealed(KDF_NONE, payload);
    const plaintexts: Uint8Array[] = [];
    const encrypt = crypto.subtle.encrypt.bind(crypto.subtle);
    const decrypt = crypto.subtle.decrypt.bind(crypto.subtle);
    const spies = [
      vi.spyOn(crypto.subtle, 'encrypt').mockImplementation(async (algorithm, key, data) => {
        if (data instanceof Uint8Array) {
          plaintexts.push(data);
        }
        return encrypt(algorithm, key, data);
      }),
      vi.spyOn(crypto.subtle, 'decrypt').mockImplementation(async (algorithm, key, data) => {
        const plaintext = await decrypt(algorithm, key, data);
        plaintexts.push(new Uint8Array(plaintext));
        return plaintext;
      }),
    ];
    let opened;
    try {
      await seal(message.contentKey, newHeader(KDF_NONE), message.id, policy, payload);
      opened = await open(message.contentKey, message.id, policy, KDF_NONE, message.object);
    } finally {
      for (const spy of spies) {
        spy.mockRestore();
      }
    }
    expect(plaintexts).toHaveLength(2);
    expect(
      plaintexts.every((plaintext) => plaintext.length > file.length && plaintext.every((byte) => byte === 0)),
    ).toBe(true);
    expect(opened.payload.file).toEqual(file);
  });
});
