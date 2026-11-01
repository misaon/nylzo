import { readFileSync } from 'node:fs';
import { describe, expect, it, vi } from 'vitest';

import { bytesToHex, hexToBytes, vectors as protocolVectors, randomId } from '../../test/vectors.ts';
import { IntegrityError, MetaError, PlaintextError } from '../errors.ts';
import { type ContentKeys, deriveContent } from '../keys/derive.ts';
import { KDF_NONE } from '../keys/kdf.ts';
import { MAX_FILE_BYTES, PAD, PREFIX_LENGTH, TAG_LENGTH } from '../limits.ts';
import { arrayBufferBacked, concatenate, utf8 } from '../primitives/bytes.ts';
import { newSecret } from '../primitives/identifiers.ts';
import { compressedLength } from './compression.ts';
import { commitment, type Expected, objectHash, open, type Payload, seal, sealWithCompression } from './envelope.ts';
import { aad, headerBytes, newHeader } from './header.ts';
import { decodeMeta, encodeMeta } from './meta.ts';

const policy = { opensMax: 1, ttlSeconds: 3600 };
const prefix = PREFIX_LENGTH;

interface Sample {
  readonly id: Uint8Array<ArrayBuffer>;
  readonly keys: ContentKeys;
  readonly header: ReturnType<typeof newHeader>;
  readonly payload: Payload;
}

async function sample(file?: Uint8Array): Promise<Sample> {
  const id = randomId();
  const keys = await deriveContent(id, newSecret(), new Uint8Array(0), newSecret());
  return {
    id,
    keys,
    header: newHeader(KDF_NONE),
    payload: {
      meta: {
        text: 'text stays outside the compressor',
        ...(file && { file: { name: 'f.bin', mime: '', size: file.length } }),
      },
      file,
    },
  };
}

async function expected(id: Uint8Array, object: Uint8Array): Promise<Expected> {
  return { id, policy, kdfId: KDF_NONE, objectHash: await objectHash(object) };
}

async function openObject(keys: ContentKeys, id: Uint8Array, object: Uint8Array): ReturnType<typeof open> {
  return open(keys, await expected(id, object), object);
}

// Seals any metadata and bytes with a valid commitment, padded to whole blocks, so a test reaches the decoder.
async function envelope(
  meta: unknown,
  file: Uint8Array,
): Promise<{ id: Uint8Array<ArrayBuffer>; keys: ContentKeys; object: Uint8Array<ArrayBuffer> }> {
  const { id, keys, header } = await sample();
  const metadata = utf8(JSON.stringify(meta));
  const length = Math.ceil((prefix + TAG_LENGTH + 4 + metadata.length + file.length) / PAD) * PAD;
  const plaintext = new Uint8Array(length - prefix - TAG_LENGTH);
  new DataView(plaintext.buffer).setUint32(0, metadata.length);
  plaintext.set(metadata, 4);
  plaintext.set(file, 4 + metadata.length);
  const committed = await commitment(keys.commitmentKey, header, id, policy);
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: arrayBufferBacked(header.iv), additionalData: aad(header, committed, id, policy) },
    keys.contentKey,
    plaintext,
  );
  return { id, keys, object: concatenate(headerBytes(header), committed, new Uint8Array(ciphertext)) };
}

describe('attachment compression on request (spec 3.2)', () => {
  it.each([128 * 1024, MAX_FILE_BYTES])(
    'reduces a compressible %i-byte file to one padded block without changing its contents',
    async (size) => {
      const file = new Uint8Array(size).fill(65);
      const { id, keys, header, payload } = await sample(file);
      const object = await sealWithCompression(keys, header, id, policy, payload);
      expect(object).toHaveLength(PAD);
      const opened = await openObject(keys, id, object);
      expect(opened.payload.meta).toEqual(payload.meta);
      expect(bytesToHex(opened.payload.file ?? new Uint8Array())).toBe(bytesToHex(file));
      const commitment = object.subarray(15, prefix);
      const plaintext = new Uint8Array(
        await crypto.subtle.decrypt(
          {
            name: 'AES-GCM',
            iv: arrayBufferBacked(header.iv),
            additionalData: aad(header, commitment, id, policy),
          },
          keys.contentKey,
          object.subarray(prefix),
        ),
      );
      const text = new TextDecoder().decode(plaintext);
      expect(text).toContain('"encoding":"gzip"');
      expect(text).toContain(payload.meta.text);
    },
  );

  it('keeps the original format when it cannot halve the length', async () => {
    const random = new Uint8Array(256 * 1024);
    for (let offset = 0; offset < random.length; offset += 65_536) {
      crypto.getRandomValues(random.subarray(offset, offset + 65_536));
    }
    for (const file of [undefined, new Uint8Array(0), new Uint8Array(40 * 1024), random]) {
      const { id, keys, header, payload } = await sample(file);
      const original = await seal(keys, header, id, policy, payload);
      expect(await sealWithCompression(keys, header, id, policy, payload)).toEqual(original);
      const opened = await openObject(keys, id, original);
      expect(opened.payload.meta).toEqual(payload.meta);
    }
  });

  it('falls back to the original format when native compression is unavailable', async () => {
    const { id, keys, header, payload } = await sample(new Uint8Array(256 * 1024));
    vi.stubGlobal('CompressionStream', undefined);
    try {
      expect(await sealWithCompression(keys, header, id, policy, payload)).toEqual(
        await seal(keys, header, id, policy, payload),
      );
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it.each(protocolVectors.compressed_length)('F6: $needed_len bytes from $original_len take $object_len', (vector) => {
    expect(compressedLength(vector.original_len, vector.needed_len)).toBe(vector.object_len);
  });

  it('authenticates before starting a decompressor', async () => {
    const { id, keys, header, payload } = await sample(new Uint8Array(256 * 1024).fill(66));
    const object = await sealWithCompression(keys, header, id, policy, payload);
    object[prefix] = (object[prefix] ?? 0) ^ 1;
    const decoder = vi.spyOn(globalThis, 'DecompressionStream');
    try {
      await expect(openObject(keys, id, object)).rejects.toBeInstanceOf(IntegrityError);
      expect(decoder).not.toHaveBeenCalled();
    } finally {
      decoder.mockRestore();
    }
  });
});

interface CompressionVectors {
  readonly pattern: string;
  readonly repetitions: number;
  readonly frames: readonly { readonly name: string; readonly gzip_hex: string }[];
  readonly rejected_frames: readonly { readonly name: string; readonly gzip_hex: string }[];
}

const vectors = JSON.parse(
  readFileSync(new URL('../../../../spec/compression-vectors.json', import.meta.url), 'utf8'),
) as CompressionVectors;

describe('cross-language compression vectors', () => {
  const expectedFile = utf8(vectors.pattern.repeat(vectors.repetitions));

  it.each(vectors.frames)('reads a frame made by $name', async (frame) => {
    const file = hexToBytes(frame.gzip_hex);
    const { id, keys, object } = await envelope(
      { v: 2, file: { name: 'rows.csv', size: expectedFile.length, encoding: 'gzip', encoded_size: file.length } },
      file,
    );
    const opened = await openObject(keys, id, object);
    expect(opened.payload.file).toEqual(expectedFile);
  });

  it.each(vectors.rejected_frames)('F5: rejects a frame with $name', async (frame) => {
    const file = hexToBytes(frame.gzip_hex);
    const { id, keys, object } = await envelope(
      { v: 2, file: { name: 'rows.csv', size: expectedFile.length, encoding: 'gzip', encoded_size: file.length } },
      file,
    );
    await expect(openObject(keys, id, object)).rejects.toBeInstanceOf(PlaintextError);
  });
});

describe('malformed compressed attachments', () => {
  const file = utf8(vectors.pattern.repeat(vectors.repetitions));
  const compressed = hexToBytes(vectors.frames[0]?.gzip_hex ?? '');
  const checksum = new Uint8Array(compressed);
  checksum[checksum.length - 8] = (checksum.at(-8) ?? 0) ^ 1;

  it.each([
    { name: 'checksum', bytes: checksum, size: file.length },
    { name: 'truncation', bytes: compressed.subarray(0, -1), size: file.length },
    { name: 'trailing bytes', bytes: concatenate(compressed, new Uint8Array([1])), size: file.length },
    { name: 'multiple frames', bytes: concatenate(compressed, compressed), size: file.length * 2 },
    { name: 'expansion bomb', bytes: compressed, size: 1024 },
    { name: 'short output', bytes: compressed, size: file.length + 1 },
  ])('rejects $name', async ({ bytes, size }) => {
    const { id, keys, object } = await envelope(
      { v: 2, file: { name: 'f', size, encoding: 'gzip', encoded_size: bytes.length } },
      bytes,
    );
    await expect(openObject(keys, id, object)).rejects.toBeInstanceOf(PlaintextError);
  });

  it.each([
    { v: 2, text: 'no file' },
    { v: 2, file: { name: 'f', size: file.length } },
    { v: 2, file: { name: 'f', size: file.length, encoding: 'zstd', encoded_size: compressed.length } },
    { v: 2, file: { name: 'f', size: MAX_FILE_BYTES + 1, encoding: 'gzip', encoded_size: compressed.length } },
    { v: 2, file: { name: 'f', size: -1, encoding: 'gzip', encoded_size: compressed.length } },
    { v: 2, file: { name: 'f', size: file.length, encoding: 'gzip', encoded_size: 0 } },
    { v: 2, file: { name: 'f', size: file.length, encoding: 'gzip', encoded_size: file.length } },
  ])('rejects invalid compression metadata %#', async (meta) => {
    const { id, keys, object } = await envelope(meta, compressed);
    await expect(openObject(keys, id, object)).rejects.toBeInstanceOf(MetaError);
    expect(() => decodeMeta(utf8(JSON.stringify(meta)))).toThrow(MetaError);
  });

  it('preserves canonical uncompressed metadata', () => {
    expect(new TextDecoder().decode(encodeMeta({ text: 'unchanged' }))).toBe('{"v":1,"text":"unchanged"}');
  });
});
