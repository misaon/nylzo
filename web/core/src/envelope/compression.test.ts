import { readFileSync } from 'node:fs';
import { describe, expect, it, vi } from 'vitest';

import { bytesToHex, hexToBytes, randomId } from '../../test/vectors.ts';
import { IntegrityError, MetaError, PlaintextError } from '../errors.ts';
import { deriveContent } from '../keys/derive.ts';
import { KDF_NONE } from '../keys/kdf.ts';
import { HEADER_LENGTH, MAX_FILE_BYTES, PAD_FILE } from '../limits.ts';
import { arrayBufferBacked, concatenate, utf8 } from '../primitives/bytes.ts';
import { newSecret } from '../primitives/identifiers.ts';
import { open, type Payload, seal, sealWithCompression } from './envelope.ts';
import { aad, newHeader } from './header.ts';
import { decodeMeta, encodeMeta } from './meta.ts';

const policy = { opensMax: 1, ttlSeconds: 3600 };

async function sample(file?: Uint8Array): Promise<{
  id: Uint8Array<ArrayBuffer>;
  key: CryptoKey;
  header: ReturnType<typeof newHeader>;
  payload: Payload;
}> {
  const id = randomId();
  const { contentKey: key } = await deriveContent(id, newSecret(), new Uint8Array(0));
  return {
    id,
    key,
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

async function envelope(
  meta: unknown,
  file: Uint8Array,
): Promise<{
  id: Uint8Array<ArrayBuffer>;
  key: CryptoKey;
  object: Uint8Array<ArrayBuffer>;
}> {
  const { id, key, header } = await sample();
  const metadata = utf8(JSON.stringify(meta));
  const plaintext = new Uint8Array(4 + metadata.length + file.length);
  new DataView(plaintext.buffer).setUint32(0, metadata.length);
  plaintext.set(metadata, 4);
  plaintext.set(file, 4 + metadata.length);
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: arrayBufferBacked(header.iv), additionalData: aad(header, id, policy) },
    key,
    plaintext,
  );
  return { id, key, object: concatenate(new Uint8Array([1, 0, 0]), header.iv, new Uint8Array(ciphertext)) };
}

describe('selective attachment compression', () => {
  it.each([128 * 1024, MAX_FILE_BYTES])(
    'reduces a compressible %i-byte file to one padded block without changing its contents',
    async (size) => {
      const file = new Uint8Array(size).fill(65);
      const { id, key, header, payload } = await sample(file);
      const object = await sealWithCompression(key, header, id, policy, payload);
      expect(object).toHaveLength(PAD_FILE);
      const opened = await open(key, id, policy, KDF_NONE, object);
      expect(opened.payload.meta).toEqual(payload.meta);
      expect(bytesToHex(opened.payload.file ?? new Uint8Array())).toBe(bytesToHex(file));
      const plaintext = new Uint8Array(
        await crypto.subtle.decrypt(
          { name: 'AES-GCM', iv: arrayBufferBacked(header.iv), additionalData: aad(header, id, policy) },
          key,
          object.subarray(HEADER_LENGTH),
        ),
      );
      const text = new TextDecoder().decode(plaintext);
      expect(text).toContain('"encoding":"gzip"');
      expect(text).toContain(payload.meta.text);
    },
  );

  it('keeps the legacy format for text, small files and incompressible files', async () => {
    const random = new Uint8Array(256 * 1024);
    for (let offset = 0; offset < random.length; offset += 65_536) {
      crypto.getRandomValues(random.subarray(offset, offset + 65_536));
    }
    for (const file of [undefined, new Uint8Array(0), new Uint8Array(100 * 1024), random]) {
      const { id, key, header, payload } = await sample(file);
      const legacy = await seal(key, header, id, policy, payload);
      expect(await sealWithCompression(key, header, id, policy, payload)).toEqual(legacy);
      const opened = await open(key, id, policy, KDF_NONE, legacy);
      expect(opened.payload.meta).toEqual(payload.meta);
    }
  });

  it('falls back to the original format when native compression is unavailable', async () => {
    const { id, key, header, payload } = await sample(new Uint8Array(256 * 1024));
    vi.stubGlobal('CompressionStream', undefined);
    try {
      expect(await sealWithCompression(key, header, id, policy, payload)).toEqual(
        await seal(key, header, id, policy, payload),
      );
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('requires ten percent savings after padding, even when gzip saves a whole block', async () => {
    const file = new Uint8Array(1024 * 1024);
    for (let offset = 32 * 1024; offset < file.length; offset += 65_536) {
      crypto.getRandomValues(file.subarray(offset, offset + 65_536));
    }
    const { id, key, header, payload } = await sample(file);
    expect(await sealWithCompression(key, header, id, policy, payload)).toEqual(
      await seal(key, header, id, policy, payload),
    );
  });

  it('authenticates before starting a decompressor', async () => {
    const { id, key, header, payload } = await sample(new Uint8Array(256 * 1024));
    const object = await sealWithCompression(key, header, id, policy, payload);
    object[HEADER_LENGTH] = (object[HEADER_LENGTH] ?? 0) ^ 1;
    const decoder = vi.spyOn(globalThis, 'DecompressionStream');
    try {
      await expect(open(key, id, policy, KDF_NONE, object)).rejects.toBeInstanceOf(IntegrityError);
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
}

const vectors = JSON.parse(
  readFileSync(new URL('../../../../spec/compression-vectors.json', import.meta.url), 'utf8'),
) as CompressionVectors;

describe('cross-language compression vectors', () => {
  it.each(vectors.frames)('reads a frame made by $name', async (frame) => {
    const expected = utf8(vectors.pattern.repeat(vectors.repetitions));
    const file = hexToBytes(frame.gzip_hex);
    const { id, key, object } = await envelope(
      { v: 2, file: { name: 'rows.csv', size: expected.length, encoding: 'gzip', encoded_size: file.length } },
      file,
    );
    const opened = await open(key, id, policy, KDF_NONE, object);
    expect(opened.payload.file).toEqual(expected);
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
    const { id, key, object } = await envelope(
      { v: 2, file: { name: 'f', size, encoding: 'gzip', encoded_size: bytes.length } },
      bytes,
    );
    await expect(open(key, id, policy, KDF_NONE, object)).rejects.toBeInstanceOf(PlaintextError);
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
    const { id, key, object } = await envelope(meta, compressed);
    await expect(open(key, id, policy, KDF_NONE, object)).rejects.toBeInstanceOf(MetaError);
    expect(() => decodeMeta(utf8(JSON.stringify(meta)))).toThrow(MetaError);
  });

  it('preserves canonical uncompressed metadata', () => {
    expect(new TextDecoder().decode(encodeMeta({ text: 'unchanged' }))).toBe('{"v":1,"text":"unchanged"}');
  });
});
