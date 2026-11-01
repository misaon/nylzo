import type { ContentKeys } from '../keys/derive.ts';
import type { KdfId } from '../keys/kdf.ts';

import { IntegrityError, KdfMismatchError, MetaError, PlaintextError, TooLargeError } from '../errors.ts';
import {
  COMMITMENT_LENGTH,
  HEADER_LENGTH,
  MAX_META_BYTES,
  MAX_OBJECT_BYTES,
  META_LEN_PREFIX,
  OBJECT_HASH_LENGTH,
  PAD,
  PREFIX_LENGTH,
  TAG_LENGTH,
} from '../limits.ts';
import { arrayBufferBacked, assertLength, concatenate, utf8 } from '../primitives/bytes.ts';
import { hash } from '../primitives/identifiers.ts';
import { compressAttachment, compressedLength, decompressAttachment } from './compression.ts';
import {
  aad,
  type Header,
  headerBytes,
  parseHeader,
  type Policy,
  policyBytes,
  validateHeader,
  validatePolicy,
} from './header.ts';
import { decodeStoredMeta, encodeMeta, encodeStoredMeta, type Meta } from './meta.ts';

const COMMITMENT_LABEL = 'nylzo/v2/commitment';
const OBJECT_HASH_LABEL = 'nylzo/v2/object';

export interface Payload {
  readonly meta: Meta;
  readonly file?: Uint8Array | undefined;
}

interface OpenedPayload extends Payload {
  readonly file?: Uint8Array<ArrayBuffer> | undefined;
}

export interface Opened {
  readonly header: Header;
  readonly payload: OpenedPayload;
}

// What a reader knows before it opens an object: its link, and the policy the server reported.
export interface Expected {
  readonly id: Uint8Array;
  readonly policy: Policy;
  readonly kdfId: KdfId;
  readonly objectHash: Uint8Array;
}

const OVERHEAD = PREFIX_LENGTH + TAG_LENGTH;

function paddedLength(length: number): number {
  return Math.ceil(length / PAD) * PAD;
}

// H of a link, which pins the object the sender made.
export async function objectHash(object: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  return hash(concatenate(utf8(OBJECT_HASH_LABEL), object));
}

function commitmentInput(header: Header, id: Uint8Array, policy: Policy): Uint8Array<ArrayBuffer> {
  return concatenate(utf8(COMMITMENT_LABEL), headerBytes(header), id, policyBytes(policy));
}

export async function commitment(
  commitmentKey: CryptoKey,
  header: Header,
  id: Uint8Array,
  policy: Policy,
): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await crypto.subtle.sign('HMAC', commitmentKey, commitmentInput(header, id, policy)));
}

export async function seal(
  content: ContentKeys,
  header: Header,
  id: Uint8Array,
  policy: Policy,
  payload: Payload,
): Promise<Uint8Array<ArrayBuffer>> {
  return sealEnvelope(content, header, id, policy, payload, false);
}

// The sender asked for compression; it is kept only when it halves the size class (spec 3.2).
export async function sealWithCompression(
  content: ContentKeys,
  header: Header,
  id: Uint8Array,
  policy: Policy,
  payload: Payload,
): Promise<Uint8Array<ArrayBuffer>> {
  return sealEnvelope(content, header, id, policy, payload, true);
}

async function sealEnvelope(
  content: ContentKeys,
  header: Header,
  id: Uint8Array,
  policy: Policy,
  payload: Payload,
  compress: boolean,
): Promise<Uint8Array<ArrayBuffer>> {
  validateHeader(header);
  validatePolicy(policy);
  const committed = await commitment(content.commitmentKey, header, id, policy);
  const plaintext = await buildPlaintext(payload, compress);
  try {
    const ciphertext = await crypto.subtle.encrypt(
      aesGcmParameters(header, committed, id, policy),
      content.contentKey,
      plaintext,
    );
    return concatenate(headerBytes(header), committed, new Uint8Array(ciphertext));
  } finally {
    plaintext.fill(0);
  }
}

// Checks the object hash, the header, the length and the commitment before it decrypts anything (spec 3).
export async function open(content: ContentKeys, expected: Expected, object: Uint8Array): Promise<Opened> {
  if (!(await hashMatches(expected.objectHash, object))) {
    throw new IntegrityError();
  }
  const header = parseHeader(object);
  if (header.kdfId !== expected.kdfId) {
    throw new KdfMismatchError();
  }
  if (object.length > MAX_OBJECT_BYTES || object.length < OVERHEAD || object.length % PAD !== 0) {
    throw new IntegrityError();
  }
  const committed = object.slice(HEADER_LENGTH, PREFIX_LENGTH);
  if (!(await commitmentMatches(content, header, expected, committed))) {
    throw new IntegrityError();
  }
  const parameters = aesGcmParameters(header, committed, expected.id, expected.policy);
  const plaintext = await decrypt(content.contentKey, parameters, object.subarray(PREFIX_LENGTH));
  try {
    return { header, payload: await parsePlaintext(plaintext) };
  } finally {
    plaintext.fill(0);
  }
}

async function hashMatches(expected: Uint8Array, object: Uint8Array): Promise<boolean> {
  if (expected.length !== OBJECT_HASH_LENGTH) {
    return false;
  }
  const actual = await objectHash(object);
  let difference = 0;
  for (const [index, byte] of actual.entries()) {
    difference |= byte ^ (expected[index] ?? 0);
  }
  return difference === 0;
}

async function commitmentMatches(
  content: ContentKeys,
  header: Header,
  expected: Expected,
  committed: Uint8Array<ArrayBuffer>,
): Promise<boolean> {
  assertLength(committed, COMMITMENT_LENGTH, 'commitment');
  return crypto.subtle.verify(
    'HMAC',
    content.commitmentKey,
    committed,
    commitmentInput(header, expected.id, expected.policy),
  );
}

function aesGcmParameters(header: Header, committed: Uint8Array, id: Uint8Array, policy: Policy): AesGcmParams {
  return {
    name: 'AES-GCM',
    iv: arrayBufferBacked(header.iv),
    additionalData: aad(header, committed, id, policy),
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
    return padContent(meta, file, paddedLength(OVERHEAD + META_LEN_PREFIX + meta.length + file.length));
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
  const contentOverhead = OVERHEAD + META_LEN_PREFIX;
  const original = paddedLength(contentOverhead + metadata.length + file.length);
  let encoded: Uint8Array<ArrayBuffer>;
  try {
    encoded = encodeStoredMeta(meta, compressed.length);
  } catch {
    return padContent(metadata, file, original);
  }
  try {
    const needed = paddedLength(contentOverhead + encoded.length + compressed.length);
    const objectLength = compressed.length < file.length ? compressedLength(original, needed) : 0;
    return objectLength === 0 ? padContent(metadata, file, original) : padContent(encoded, compressed, objectLength);
  } finally {
    encoded.fill(0);
  }
}

function padContent(meta: Uint8Array, file: Uint8Array, objectLength: number): Uint8Array<ArrayBuffer> {
  if (objectLength > MAX_OBJECT_BYTES) {
    throw new TooLargeError();
  }
  const plaintext = new Uint8Array(objectLength - OVERHEAD);
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
