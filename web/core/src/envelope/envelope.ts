import type { KdfId } from '../keys/kdf.ts';

import { IntegrityError, KdfMismatchError, MetaError, PlaintextError, TooLargeError } from '../errors.ts';
import {
  HEADER_LENGTH,
  MAX_META_BYTES,
  MAX_OBJECT_BYTES,
  META_LEN_PREFIX,
  PAD_FILE,
  PAD_TEXT,
  TAG_LENGTH,
} from '../limits.ts';
import { arrayBufferBacked, concatenate } from '../primitives/bytes.ts';
import { compressAttachment, decompressAttachment } from './compression.ts';
import { aad, type Header, headerBytes, parseHeader, type Policy, validateHeader, validatePolicy } from './header.ts';
import { decodeStoredMeta, encodeMeta, encodeStoredMeta, type Meta } from './meta.ts';

export interface Payload {
  readonly meta: Meta;
  readonly file?: Uint8Array | undefined;
}

export interface OpenedPayload extends Payload {
  readonly file?: Uint8Array<ArrayBuffer> | undefined;
}

export interface Opened {
  readonly header: Header;
  readonly payload: OpenedPayload;
}

function paddedLength(length: number, hasFile: boolean): number {
  const unit = hasFile ? PAD_FILE : PAD_TEXT;
  return Math.ceil(length / unit) * unit;
}

export async function seal(
  contentKey: CryptoKey,
  header: Header,
  id: Uint8Array,
  policy: Policy,
  payload: Payload,
): Promise<Uint8Array<ArrayBuffer>> {
  return sealEnvelope(contentKey, header, id, policy, payload, false);
}

export async function sealWithCompression(
  contentKey: CryptoKey,
  header: Header,
  id: Uint8Array,
  policy: Policy,
  payload: Payload,
): Promise<Uint8Array<ArrayBuffer>> {
  return sealEnvelope(contentKey, header, id, policy, payload, true);
}

async function sealEnvelope(
  contentKey: CryptoKey,
  header: Header,
  id: Uint8Array,
  policy: Policy,
  payload: Payload,
  compress: boolean,
): Promise<Uint8Array<ArrayBuffer>> {
  validateHeader(header);
  validatePolicy(policy);
  const plaintext = await buildPlaintext(payload, compress);
  try {
    const ciphertext = await crypto.subtle.encrypt(aesGcmParameters(header, id, policy), contentKey, plaintext);
    return concatenate(headerBytes(header), new Uint8Array(ciphertext));
  } finally {
    plaintext.fill(0);
  }
}

export async function open(
  contentKey: CryptoKey,
  id: Uint8Array,
  policy: Policy,
  kdfId: KdfId,
  object: Uint8Array,
): Promise<Opened> {
  const header = parseHeader(object);
  if (header.kdfId !== kdfId) {
    throw new KdfMismatchError();
  }
  if (object.length > MAX_OBJECT_BYTES || object.length < HEADER_LENGTH + TAG_LENGTH) {
    throw new IntegrityError();
  }
  const parameters = aesGcmParameters(header, id, policy);
  const plaintext = await decrypt(contentKey, parameters, object.subarray(HEADER_LENGTH));
  try {
    return { header, payload: await parsePlaintext(plaintext) };
  } finally {
    plaintext.fill(0);
  }
}

function aesGcmParameters(header: Header, id: Uint8Array, policy: Policy): AesGcmParams {
  return {
    name: 'AES-GCM',
    iv: arrayBufferBacked(header.iv),
    additionalData: aad(header, id, policy),
    tagLength: TAG_LENGTH * 8,
  };
}

async function decrypt(
  contentKey: CryptoKey,
  parameters: AesGcmParams,
  ciphertext: Uint8Array,
): Promise<Uint8Array<ArrayBuffer>> {
  try {
    return new Uint8Array(await crypto.subtle.decrypt(parameters, contentKey, arrayBufferBacked(ciphertext)));
  } catch {
    throw new IntegrityError();
  }
}

async function buildPlaintext(payload: Payload, compress: boolean): Promise<Uint8Array<ArrayBuffer>> {
  const meta = encodeMeta(payload.meta);
  try {
    const file = payload.file ?? new Uint8Array(0);
    if (file.length !== (payload.meta.file?.size ?? 0)) {
      throw new MetaError('file length differs from meta.file.size');
    }
    if (compress && payload.meta.file !== undefined) {
      const compressed = await compressAttachment(file);
      if (compressed !== undefined) {
        try {
          return compressedPlaintext(payload.meta, meta, file, compressed);
        } finally {
          compressed.fill(0);
        }
      }
    }
    return padContent(meta, file, payload.meta.file !== undefined);
  } finally {
    meta.fill(0);
  }
}

function compressedPlaintext(
  meta: Meta,
  metadata: Uint8Array,
  file: Uint8Array,
  compressed: Uint8Array,
): Uint8Array<ArrayBuffer> {
  let encoded: Uint8Array<ArrayBuffer>;
  try {
    encoded = encodeStoredMeta(meta, compressed.length);
  } catch {
    return padContent(metadata, file, true);
  }
  try {
    const overhead = HEADER_LENGTH + TAG_LENGTH + META_LEN_PREFIX;
    const originalSize = paddedLength(overhead + metadata.length + file.length, true);
    const compressedSize = paddedLength(overhead + encoded.length + compressed.length, true);
    return compressed.length >= file.length ||
      originalSize - compressedSize < PAD_FILE ||
      compressedSize * 10 > originalSize * 9
      ? padContent(metadata, file, true)
      : padContent(encoded, compressed, true);
  } finally {
    encoded.fill(0);
  }
}

function padContent(meta: Uint8Array, file: Uint8Array, hasFile: boolean): Uint8Array<ArrayBuffer> {
  const overhead = HEADER_LENGTH + TAG_LENGTH;
  const content = META_LEN_PREFIX + meta.length + file.length;
  const length = paddedLength(overhead + content, hasFile) - overhead;
  if (overhead + length > MAX_OBJECT_BYTES) {
    throw new TooLargeError();
  }
  const plaintext = new Uint8Array(length);
  new DataView(plaintext.buffer).setUint32(0, meta.length);
  plaintext.set(meta, META_LEN_PREFIX);
  plaintext.set(file, META_LEN_PREFIX + meta.length);
  return plaintext;
}

async function parsePlaintext(plaintext: Uint8Array<ArrayBuffer>): Promise<OpenedPayload> {
  if (plaintext.length < META_LEN_PREFIX) {
    throw new PlaintextError();
  }
  const metaLength = new DataView(plaintext.buffer, plaintext.byteOffset, plaintext.byteLength).getUint32(0);
  const fileStart = META_LEN_PREFIX + metaLength;
  if (metaLength > MAX_META_BYTES || fileStart > plaintext.length) {
    throw new PlaintextError();
  }
  const { meta, compressedSize } = decodeStoredMeta(plaintext.subarray(META_LEN_PREFIX, fileStart));
  if (meta.file === undefined) {
    return { meta };
  }
  const size = compressedSize ?? meta.file.size;
  if (size > plaintext.length - fileStart) {
    throw new PlaintextError();
  }
  const file =
    compressedSize === undefined
      ? plaintext.slice(fileStart, fileStart + size)
      : await decompressAttachment(plaintext.subarray(fileStart, fileStart + size), meta.file.size);
  return { meta, file };
}
