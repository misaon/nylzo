import { FlagsError, HeaderError, KdfError, PolicyError, VersionError } from '../errors.ts';
import { isKdfId, type KdfId } from '../keys/kdf.ts';
import { HEADER_LEN, ID_LEN, isUnsignedInteger, IV_LEN, MIN_TTL_SECONDS, POLICY_LEN, VERSION } from '../limits.ts';
import { assertLength, concatenate } from '../primitives/bytes.ts';

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

const IV_OFFSET = 3;
const MAX_OPENS = 2 ** 16 - 1;
const MAX_TTL_SECONDS = 2 ** 32 - 1;

export function newHeader(kdfId: KdfId): Header {
  return { version: VERSION, kdfId, flags: 0, iv: crypto.getRandomValues(new Uint8Array(IV_LEN)) };
}

export function headerBytes(header: Header): Uint8Array<ArrayBuffer> {
  assertIvLength(header.iv);
  const bytes = new Uint8Array(HEADER_LEN);
  bytes.set([header.version, header.kdfId, header.flags]);
  bytes.set(header.iv, IV_OFFSET);
  return bytes;
}

export function validateHeader(header: Header): void {
  checkHeaderFields(header.version, header.kdfId, header.flags);
  assertIvLength(header.iv);
}

export function parseHeader(bytes: Uint8Array): Header {
  if (bytes.length < HEADER_LEN) {
    throw new HeaderError();
  }
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const kdfId = checkHeaderFields(view.getUint8(0), view.getUint8(1), view.getUint8(2));
  return { version: VERSION, kdfId, flags: 0, iv: bytes.slice(IV_OFFSET, HEADER_LEN) };
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
  return concatenate(headerBytes(header), id, policyBytes(policy));
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

function assertIvLength(iv: Uint8Array): void {
  if (iv.length !== IV_LEN) {
    throw new HeaderError(`iv must be ${IV_LEN} bytes`);
  }
}

function assertPolicyRange(policy: Policy): void {
  if (!isUnsignedInteger(policy.opensMax, MAX_OPENS) || !isUnsignedInteger(policy.ttlSeconds, MAX_TTL_SECONDS)) {
    throw new PolicyError('policy value out of range');
  }
}
