import { arrayBufferBacked, assertLength, concat } from './bytes.ts';
import {
  FlagsError,
  HeaderError,
  IntegrityError,
  KdfError,
  KdfMismatchError,
  MetaError,
  PlaintextError,
  PolicyError,
  TooLargeError,
  VersionError,
} from './errors.ts';
import { isKdfId, type KdfId } from './kdf.ts';
import {
  HEADER_LEN,
  ID_LEN,
  IV_LEN,
  MAX_META_BYTES,
  MAX_OBJECT_BYTES,
  META_LEN_PREFIX,
  MIN_TTL_SECONDS,
  PAD_FILE,
  PAD_TEXT,
  POLICY_LEN,
  TAG_LEN,
  VERSION,
} from './limits.ts';
import { decodeMeta, encodeMeta, type Meta } from './meta.ts';

export interface Header {
  readonly version: number;
  readonly kdfId: KdfId;
  readonly flags: number;
  readonly iv: Uint8Array;
}

export interface Policy {
  readonly opensMax: number;
  readonly ttlSeconds: number;
}

export interface Payload {
  readonly meta: Meta;
  readonly file?: Uint8Array | undefined;
}

export interface Opened {
  readonly header: Header;
  readonly payload: Payload;
}

const MAX_OPENS = 0xffff;
const MAX_TTL_SECONDS = 0xffffffff;
const GCM_TAG_BITS = TAG_LEN * 8;

export function newIv(): Uint8Array<ArrayBuffer> {
  return crypto.getRandomValues(new Uint8Array(IV_LEN));
}

export function newHeader(kdfId: KdfId): Header {
  return { version: VERSION, kdfId, flags: 0, iv: newIv() };
}

export function headerBytes(header: Header): Uint8Array<ArrayBuffer> {
  if (header.iv.length !== IV_LEN) {
    throw new HeaderError(`iv must be ${IV_LEN} bytes`);
  }
  const bytes = new Uint8Array(HEADER_LEN);
  bytes[0] = header.version;
  bytes[1] = header.kdfId;
  bytes[2] = header.flags;
  bytes.set(header.iv, 3);
  return bytes;
}

export function validateHeader(header: Header): void {
  checkHeaderFields(header.version, header.kdfId, header.flags);
  if (header.iv.length !== IV_LEN) {
    throw new HeaderError(`iv must be ${IV_LEN} bytes`);
  }
}

export function parseHeader(bytes: Uint8Array): Header {
  if (bytes.length < HEADER_LEN) {
    throw new HeaderError();
  }
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const kdfId = checkHeaderFields(view.getUint8(0), view.getUint8(1), view.getUint8(2));
  return { version: VERSION, kdfId, flags: 0, iv: bytes.slice(3, HEADER_LEN) };
}

export function validatePolicy(policy: Policy): void {
  assertPolicyRange(policy);
  if (policy.opensMax === 0 || policy.ttlSeconds < MIN_TTL_SECONDS) {
    throw new PolicyError();
  }
}

export function policyBytes(policy: Policy): Uint8Array<ArrayBuffer> {
  assertPolicyRange(policy);
  const bytes = new Uint8Array(POLICY_LEN);
  const view = new DataView(bytes.buffer);
  view.setUint16(0, policy.opensMax);
  view.setUint32(2, policy.ttlSeconds);
  return bytes;
}

export function aad(header: Header, id: Uint8Array, policy: Policy): Uint8Array<ArrayBuffer> {
  assertLength(id, ID_LEN, 'id');
  return concat(headerBytes(header), id, policyBytes(policy));
}

export function paddedLen(n: number, hasFile: boolean): number {
  const unit = hasFile ? PAD_FILE : PAD_TEXT;
  const remainder = n % unit;
  return remainder === 0 ? n : n + unit - remainder;
}

export async function seal(
  k: CryptoKey,
  header: Header,
  id: Uint8Array,
  policy: Policy,
  payload: Payload,
): Promise<Uint8Array<ArrayBuffer>> {
  validateHeader(header);
  validatePolicy(policy);
  const plaintext = buildPlaintext(payload);
  if (HEADER_LEN + plaintext.length + TAG_LEN > MAX_OBJECT_BYTES) {
    throw new TooLargeError();
  }
  const head = headerBytes(header);
  const params: AesGcmParams = {
    name: 'AES-GCM',
    iv: head.subarray(3, HEADER_LEN),
    additionalData: aad(header, id, policy),
    tagLength: GCM_TAG_BITS,
  };
  const ciphertext = await crypto.subtle.encrypt(params, k, plaintext);
  return concat(head, new Uint8Array(ciphertext));
}

export async function open(
  k: CryptoKey,
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
  const plaintext = await decrypt(k, header, id, policy, object.subarray(HEADER_LEN));
  return { header, payload: parsePlaintext(plaintext) };
}

function checkHeaderFields(version: number, kdfId: number, flags: number): KdfId {
  if (version !== VERSION) {
    throw new VersionError();
  }
  if (flags !== 0) {
    throw new FlagsError();
  }
  if (!isKdfId(kdfId)) {
    throw new KdfError();
  }
  return kdfId;
}

function assertPolicyRange(policy: Policy): void {
  if (!isUint(policy.opensMax, MAX_OPENS) || !isUint(policy.ttlSeconds, MAX_TTL_SECONDS)) {
    throw new PolicyError('policy value out of range');
  }
}

function isUint(value: number, max: number): boolean {
  return Number.isInteger(value) && value >= 0 && value <= max;
}

function buildPlaintext(payload: Payload): Uint8Array<ArrayBuffer> {
  const meta = encodeMeta(payload.meta);
  const file = payload.file ?? new Uint8Array(0);
  const fileMeta = payload.meta.file;
  const expectedSize = fileMeta === undefined ? 0 : fileMeta.size;
  if (file.length !== expectedSize) {
    throw new MetaError('file length differs from meta.file.size');
  }
  const plaintext = new Uint8Array(paddedLen(META_LEN_PREFIX + meta.length + file.length, fileMeta !== undefined));
  new DataView(plaintext.buffer).setUint32(0, meta.length);
  plaintext.set(meta, META_LEN_PREFIX);
  plaintext.set(file, META_LEN_PREFIX + meta.length);
  return plaintext;
}

async function decrypt(
  k: CryptoKey,
  header: Header,
  id: Uint8Array,
  policy: Policy,
  ciphertext: Uint8Array,
): Promise<Uint8Array<ArrayBuffer>> {
  const params: AesGcmParams = {
    name: 'AES-GCM',
    iv: arrayBufferBacked(header.iv),
    additionalData: aad(header, id, policy),
    tagLength: GCM_TAG_BITS,
  };
  try {
    return new Uint8Array(await crypto.subtle.decrypt(params, k, arrayBufferBacked(ciphertext)));
  } catch {
    throw new IntegrityError();
  }
}

function parsePlaintext(plaintext: Uint8Array<ArrayBuffer>): Payload {
  if (plaintext.length < META_LEN_PREFIX) {
    throw new PlaintextError();
  }
  const lenMeta = new DataView(plaintext.buffer, plaintext.byteOffset, plaintext.byteLength).getUint32(0);
  if (lenMeta > MAX_META_BYTES || lenMeta > plaintext.length - META_LEN_PREFIX) {
    throw new PlaintextError();
  }
  const meta = decodeMeta(plaintext.subarray(META_LEN_PREFIX, META_LEN_PREFIX + lenMeta));
  if (meta.file === undefined) {
    return { meta };
  }
  const start = META_LEN_PREFIX + lenMeta;
  if (meta.file.size > plaintext.length - start) {
    throw new PlaintextError();
  }
  return { meta, file: plaintext.slice(start, start + meta.file.size) };
}
