import { beforeAll, describe, expect, it, vi } from 'vitest';

import { bytesToHex, fileBytes, hexToBytes, kdfIdOf, randomId, sha256Hex, vectors } from '../../test/vectors.ts';
import {
  FlagsError,
  HeaderError,
  IntegrityError,
  KdfError,
  KdfMismatchError,
  MetaError,
  PlaintextError,
  PolicyError,
  VersionError,
} from '../errors.ts';
import { type ContentKeys, deriveContent } from '../keys/derive.ts';
import { KDF_NONE, KDF_RETIRED, KDF_SCRYPT, type KdfId } from '../keys/kdf.ts';
import {
  HEADER_LENGTH,
  IV_LENGTH,
  MAX_FILE_BYTES,
  MAX_OBJECT_BYTES,
  MAX_TEXT_BYTES,
  PAD,
  PREFIX_LENGTH,
  TAG_LENGTH,
} from '../limits.ts';
import { decodeBase64url, encodeBase64url } from '../primitives/base64url.ts';
import { newSecret, parseId } from '../primitives/identifiers.ts';
import { deriveAll } from '../scrypt.ts';
import { type Expected, objectHash, open, type Payload, seal } from './envelope.ts';
import { aad, type Header, newHeader, type Policy } from './header.ts';
import { encodeMeta, type Meta } from './meta.ts';

interface Sealed {
  readonly id: Uint8Array<ArrayBuffer>;
  readonly secret: Uint8Array<ArrayBuffer>;
  readonly share: Uint8Array<ArrayBuffer>;
  readonly keys: ContentKeys;
  readonly object: Uint8Array<ArrayBuffer>;
}

const policy: Policy = { opensMax: 1, ttlSeconds: 86_400 };
const textPayload: Payload = { meta: { text: 'integrity' } };
const passphraseKey = new Uint8Array(32).fill(1);
const prefix = PREFIX_LENGTH;

async function keysOf(id: Uint8Array, secret: Uint8Array, key: Uint8Array, share: Uint8Array): Promise<ContentKeys> {
  return deriveContent(id, secret, key, share);
}

async function sealed(kdfId: KdfId, payload = textPayload): Promise<Sealed> {
  const id = randomId();
  const secret = newSecret();
  const share = newSecret();
  const keys = await keysOf(id, secret, kdfId === KDF_NONE ? new Uint8Array(0) : passphraseKey, share);
  const object = await seal(keys, newHeader(kdfId), id, policy, payload);
  return { id, secret, share, keys, object };
}

async function expectedFor(
  object: Uint8Array,
  id: Uint8Array,
  kdfId: KdfId,
  changed: Partial<Expected> = {},
): Promise<Expected> {
  return { id, policy, kdfId, objectHash: await objectHash(object), ...changed };
}

function flipped(bytes: Uint8Array, index: number, mask = 1): Uint8Array<ArrayBuffer> {
  const copy = Uint8Array.from(bytes);
  copy[index] = (copy[index] ?? 0) ^ mask;
  return copy;
}

async function digestOf(bytes: Uint8Array | undefined): Promise<string | undefined> {
  return bytes && (await sha256Hex(bytes));
}

type EnvelopeVector = (typeof vectors.envelope)[number];

async function sealVector(vector: EnvelopeVector): Promise<{
  readonly object: Uint8Array<ArrayBuffer>;
  readonly keys: ContentKeys;
  readonly header: Header;
  readonly meta: Meta;
  readonly file: Uint8Array<ArrayBuffer> | undefined;
}> {
  const id = parseId(vector.id);
  const kdfId = kdfIdOf(vector.kdf_id);
  const share = hexToBytes(vector.e_hex);
  const { content } = await deriveAll(id, hexToBytes(vector.s), kdfId, vector.passphrase ?? '', share);
  const header: Header = { version: 2, kdfId, flags: vector.flags, iv: hexToBytes(vector.iv_hex) };
  const file = vector.file && fileBytes(vector.file);
  const fileMeta = vector.file && { name: vector.file.name, mime: vector.file.mime, size: vector.file.size };
  const meta: Meta = { text: vector.text, file: fileMeta };
  const object = await seal(
    content,
    header,
    id,
    { opensMax: vector.opens_max, ttlSeconds: vector.ttl_s },
    { meta, file },
  );
  return { object, keys: content, header, meta, file };
}

describe('AK9: envelope vectors', () => {
  it.each(vectors.envelope)('$name', { timeout: 120_000 }, async (vector) => {
    const id = parseId(vector.id);
    const vectorPolicy: Policy = { opensMax: vector.opens_max, ttlSeconds: vector.ttl_s };
    const { object, keys, header, meta, file } = await sealVector(vector);

    expect(new TextDecoder().decode(encodeMeta(meta))).toBe(vector.meta_json);
    const committed = object.subarray(HEADER_LENGTH, PREFIX_LENGTH);
    expect(bytesToHex(aad(header, committed, id, vectorPolicy))).toBe(vector.aad_hex);
    expect(bytesToHex(committed)).toBe(vector.commitment_hex);
    expect(object.length).toBe(vector.object_len);
    expect(object.length - PREFIX_LENGTH - TAG_LENGTH).toBe(vector.plaintext_len);
    expect(await sha256Hex(object)).toBe(vector.object_sha256);
    expect(vector.object_b64 === undefined ? undefined : encodeBase64url(object)).toBe(vector.object_b64);

    const hash = await objectHash(object);
    expect(encodeBase64url(hash)).toBe(vector.h);
    const expected: Expected = { id, policy: vectorPolicy, kdfId: header.kdfId, objectHash: hash };
    const opened = await open(keys, expected, object);
    expect(opened.header).toEqual(header);
    expect(opened.payload.meta).toEqual(meta);
    expect(await digestOf(opened.payload.file)).toBe(await digestOf(file));
  });
});

const rejections = {
  flags: FlagsError,
  integrity: IntegrityError,
  kdf_mismatch: KdfMismatchError,
  plaintext: PlaintextError,
  version: VersionError,
} as const;

describe('AK9, F1, F3, F4: a reader refuses every object its link does not open', () => {
  const byName = new Map(vectors.envelope.map((vector) => [vector.name, vector]));

  it.each(vectors.envelope_rejected)('$name fails with $error', { timeout: 120_000 }, async (vector) => {
    const base = byName.get(vector.base);
    if (base === undefined) {
      throw new Error(`no envelope vector ${vector.base}`);
    }
    let object: Uint8Array<ArrayBuffer>;
    if (vector.object_b64 === undefined) {
      const source = byName.get(vector.object_from ?? vector.base);
      if (source === undefined) {
        throw new Error(`no envelope vector ${vector.object_from ?? ''}`);
      }
      const sealedSource = await sealVector(source);
      object = sealedSource.object;
    } else {
      object = decodeBase64url(vector.object_b64);
    }
    if (vector.flip_byte !== undefined) {
      object = flipped(object, vector.flip_byte);
    }
    if (vector.truncate_to !== undefined) {
      object = object.slice(0, vector.truncate_to);
    }
    const id = parseId(base.id);
    const kdfId = kdfIdOf(vector.kdf_id);
    const { content } = await deriveAll(
      id,
      hexToBytes(base.s),
      kdfId,
      vector.passphrase ?? '',
      hexToBytes(vector.e_hex),
    );
    const expected: Expected = {
      id,
      policy: { opensMax: base.opens_max, ttlSeconds: base.ttl_s },
      kdfId,
      objectHash: decodeBase64url(vector.h),
    };
    await expect(open(content, expected, object)).rejects.toBeInstanceOf(rejections[vector.error]);
  });
});

describe('AK8: a changed envelope, policy or key fails integrity', () => {
  let message: Sealed;
  beforeAll(async () => {
    message = await sealed(KDF_SCRYPT);
  });

  const reopen = async (
    object: Uint8Array,
    changed: Partial<Expected> = {},
    keys: ContentKeys = message.keys,
  ): ReturnType<typeof open> =>
    open(keys, await expectedFor(object, changed.id ?? message.id, KDF_SCRYPT, changed), object);

  it.each<[string, () => Promise<unknown>]>([
    ['a flipped ciphertext bit', () => reopen(flipped(message.object, prefix))],
    ['a flipped bit in the middle', () => reopen(flipped(message.object, prefix + 100))],
    ['a flipped tag bit', () => reopen(flipped(message.object, message.object.length - 1))],
    ['a flipped iv bit', () => reopen(flipped(message.object, 5))],
    ['a flipped commitment bit', () => reopen(flipped(message.object, HEADER_LENGTH))],
    ['a raised opens_max', () => reopen(message.object, { policy: { ...policy, opensMax: 2 } })],
    ['a longer TTL', () => reopen(message.object, { policy: { ...policy, ttlSeconds: policy.ttlSeconds + 1 } })],
    ['another id', () => reopen(message.object, { id: randomId() })],
    [
      'another secret',
      async () => reopen(message.object, {}, await keysOf(message.id, newSecret(), passphraseKey, message.share)),
    ],
    [
      'a wrong passphrase',
      async () =>
        reopen(message.object, {}, await keysOf(message.id, message.secret, new Uint8Array(32), message.share)),
    ],
    [
      'F3: another share',
      async () => reopen(message.object, {}, await keysOf(message.id, message.secret, passphraseKey, newSecret())),
    ],
    [
      'F1: an object other than the link pins',
      async () =>
        open(
          message.keys,
          await expectedFor(message.object, message.id, KDF_SCRYPT, { objectHash: newSecret() }),
          message.object,
        ),
    ],
    [
      'F1: an empty object hash',
      async () =>
        open(
          message.keys,
          await expectedFor(message.object, message.id, KDF_SCRYPT, { objectHash: new Uint8Array(0) }),
          message.object,
        ),
    ],
  ])('%s', async (_, tamper) => {
    await expect(tamper()).rejects.toBeInstanceOf(IntegrityError);
  });
});

describe('open rejects a malformed header before decrypting (spec 3)', () => {
  let message: Sealed;
  beforeAll(async () => {
    message = await sealed(KDF_NONE);
  });

  it.each<[string, (object: Uint8Array) => Uint8Array, KdfId, new () => Error]>([
    ['version 3', (object) => flipped(object, 0, 1), KDF_NONE, VersionError],
    ['version 1', (object) => flipped(object, 0, 3), KDF_NONE, VersionError],
    ['flags 1', (object) => flipped(object, 2), KDF_NONE, FlagsError],
    ['the retired profile 2', (object) => flipped(object, 1, KDF_RETIRED), KDF_RETIRED as KdfId, KdfError],
    ['a profile other than the fragment', (object) => object, KDF_SCRYPT, KdfMismatchError],
    ['an object without a whole tag', (object) => object.subarray(0, prefix + 3), KDF_NONE, IntegrityError],
    ['an object that is not whole blocks', (object) => object.subarray(0, -1), KDF_NONE, IntegrityError],
    ['a truncated header', (object) => object.subarray(0, HEADER_LENGTH - 1), KDF_NONE, HeaderError],
  ])('%s', async (_, change, kdfId, expected) => {
    const object = change(message.object);
    await expect(open(message.keys, await expectedFor(object, message.id, kdfId), object)).rejects.toBeInstanceOf(
      expected,
    );
  });
});

describe('seal rejects what the spec forbids (spec 3 and 3.1)', () => {
  const header = newHeader(KDF_NONE);
  const oversizedFile = new Uint8Array(MAX_FILE_BYTES + 1);
  let keys: ContentKeys;
  beforeAll(async () => {
    keys = await keysOf(randomId(), newSecret(), new Uint8Array(0), newSecret());
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
    ['the retired profile 2', { ...header, kdfId: KDF_RETIRED as KdfId }, policy, textPayload, KdfError],
    ['an 11-byte iv', { ...header, iv: new Uint8Array(11) }, policy, textPayload, HeaderError],
  ])('%s', async (_, invalidHeader, invalidPolicy, payload, expected) => {
    await expect(seal(keys, invalidHeader, randomId(), invalidPolicy, payload)).rejects.toBeInstanceOf(expected);
  });

  it('F7: pads a text and a file to the same length', async () => {
    const text = await sealed(KDF_NONE, { meta: { text: 't'.repeat(3000) } });
    const file = new Uint8Array(3000).fill(7);
    const attachment = await sealed(KDF_NONE, { meta: { file: { name: 'f', mime: '', size: file.length } }, file });
    expect(text.object).toHaveLength(PAD);
    expect(attachment.object).toHaveLength(PAD);
  });

  it('fits the largest message into MAX_OBJECT_BYTES', { timeout: 60_000 }, async () => {
    const file = new Uint8Array(MAX_FILE_BYTES).fill(7);
    const text = 'y'.repeat(MAX_TEXT_BYTES);
    const meta = { text, file: { name: 'max.bin', mime: 'application/octet-stream', size: file.length } };
    const message = await sealed(KDF_NONE, { meta, file });
    expect(message.object.length).toBeLessThanOrEqual(MAX_OBJECT_BYTES);
    const opened = await open(message.keys, await expectedFor(message.object, message.id, KDF_NONE), message.object);
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
      await seal(message.keys, newHeader(KDF_NONE), message.id, policy, payload);
      opened = await open(message.keys, await expectedFor(message.object, message.id, KDF_NONE), message.object);
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
