import type { KdfId } from '../keys/kdf.ts';

import { IntegrityError, KdfMismatchError, MetaError, PlaintextError, TooLargeError } from '../errors.ts';
import {
  HEADER_LEN,
  MAX_META_BYTES,
  MAX_OBJECT_BYTES,
  META_LEN_PREFIX,
  PAD_FILE,
  PAD_TEXT,
  TAG_LEN,
} from '../limits.ts';
import { arrayBufferBacked, concatenate } from '../primitives/bytes.ts';
import { aad, type Header, headerBytes, parseHeader, type Policy, validateHeader, validatePolicy } from './header.ts';
import { decodeMeta, encodeMeta, type Meta } from './meta.ts';

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
  validateHeader(header);
  validatePolicy(policy);
  const plaintext = buildPlaintext(payload);
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
  if (object.length > MAX_OBJECT_BYTES || object.length < HEADER_LEN + TAG_LEN) {
    throw new IntegrityError();
  }
  const parameters = aesGcmParameters(header, id, policy);
  const plaintext = await decrypt(contentKey, parameters, object.subarray(HEADER_LEN));
  try {
    return { header, payload: parsePlaintext(plaintext) };
  } finally {
    plaintext.fill(0);
  }
}

function aesGcmParameters(header: Header, id: Uint8Array, policy: Policy): AesGcmParams {
  return {
    name: 'AES-GCM',
    iv: arrayBufferBacked(header.iv),
    additionalData: aad(header, id, policy),
    tagLength: TAG_LEN * 8,
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

function buildPlaintext(payload: Payload): Uint8Array<ArrayBuffer> {
  const meta = encodeMeta(payload.meta);
  const file = payload.file ?? new Uint8Array(0);
  if (file.length !== (payload.meta.file?.size ?? 0)) {
    throw new MetaError('file length differs from meta.file.size');
  }
  const length = paddedLength(META_LEN_PREFIX + meta.length + file.length, payload.meta.file !== undefined);
  if (HEADER_LEN + length + TAG_LEN > MAX_OBJECT_BYTES) {
    throw new TooLargeError();
  }
  const plaintext = new Uint8Array(length);
  new DataView(plaintext.buffer).setUint32(0, meta.length);
  plaintext.set(meta, META_LEN_PREFIX);
  plaintext.set(file, META_LEN_PREFIX + meta.length);
  return plaintext;
}

function parsePlaintext(plaintext: Uint8Array<ArrayBuffer>): OpenedPayload {
  if (plaintext.length < META_LEN_PREFIX) {
    throw new PlaintextError();
  }
  const metaLength = new DataView(plaintext.buffer, plaintext.byteOffset, plaintext.byteLength).getUint32(0);
  const fileStart = META_LEN_PREFIX + metaLength;
  if (metaLength > MAX_META_BYTES || fileStart > plaintext.length) {
    throw new PlaintextError();
  }
  const meta = decodeMeta(plaintext.subarray(META_LEN_PREFIX, fileStart));
  if (meta.file === undefined) {
    return { meta };
  }
  if (meta.file.size > plaintext.length - fileStart) {
    throw new PlaintextError();
  }
  return { meta, file: plaintext.slice(fileStart, fileStart + meta.file.size) };
}
