import { FlagsError, HeaderError, KdfError, PolicyError, VersionError } from '../errors.ts';
import { isKdfId } from '../keys/kdf.ts';
import type { KdfId } from '../keys/kdf.ts';
import {
  FLAG_DEADLINE,
  HEADER_LENGTH,
  ID_LENGTH,
  isUnsignedInteger,
  IV_LENGTH,
  MIN_TTL_SECONDS,
  POLICY_LENGTH,
  VERSION,
} from '../limits.ts';
import { assertLength, concatenate } from '../primitives/bytes.ts';

export interface Header {
  readonly version: typeof VERSION;
  readonly kdfId: KdfId;
  readonly flags: number;
  readonly iv: Uint8Array;
}

export interface Policy {
  readonly opensMax: number;
  readonly ttlSeconds: number;
  // When the message expires, in Unix seconds.
  readonly deadline: number;
}

const IV_OFFSET = 3;
const DEADLINE_OFFSET = 6;
const MAX_OPENS = 2 ** 16 - 1;
const MAX_UINT32 = 2 ** 32 - 1;

export function newHeader(kdfId: KdfId): Header {
  return { version: VERSION, kdfId, flags: FLAG_DEADLINE, iv: crypto.getRandomValues(new Uint8Array(IV_LENGTH)) };
}

export function headerBytes(header: Header): Uint8Array<ArrayBuffer> {
  assertIvLength(header.iv);
  const bytes = new Uint8Array(HEADER_LENGTH);
  bytes.set([header.version, header.kdfId, header.flags]);
  bytes.set(header.iv, IV_OFFSET);
  return bytes;
}

export function validateHeader(header: Header): void {
  checkHeaderFields(header.version, header.kdfId, header.flags);
  assertIvLength(header.iv);
}

export function parseHeader(bytes: Uint8Array): Header {
  if (bytes.length < HEADER_LENGTH) {
    throw new HeaderError();
  }
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const flags = view.getUint8(2);
  const kdfId = checkHeaderFields(view.getUint8(0), view.getUint8(1), flags);
  return {
    version: VERSION,
    kdfId,
    flags,
    iv: bytes.slice(IV_OFFSET, HEADER_LENGTH),
  };
}

export function validatePolicy(policy: Policy): void {
  assertPolicyRange(policy);
  if (policy.opensMax === 0 || policy.ttlSeconds < MIN_TTL_SECONDS || policy.deadline === 0) {
    throw new PolicyError();
  }
}

export function policyBytes(policy: Policy): Uint8Array<ArrayBuffer> {
  assertPolicyRange(policy);
  const bytes = new Uint8Array(POLICY_LENGTH);
  const view = new DataView(bytes.buffer);
  view.setUint16(0, policy.opensMax);
  view.setUint32(2, policy.ttlSeconds);
  view.setUint32(DEADLINE_OFFSET, policy.deadline);
  return bytes;
}

export function aad(header: Header, commitment: Uint8Array, id: Uint8Array, policy: Policy): Uint8Array<ArrayBuffer> {
  assertLength(id, ID_LENGTH, 'id');
  return concatenate(headerBytes(header), commitment, id, policyBytes(policy));
}

function checkHeaderFields(version: number, kdfId: number, flags: number): KdfId {
  if (version !== VERSION) {
    throw new VersionError();
  }
  if (flags !== FLAG_DEADLINE) {
    throw new FlagsError();
  }
  if (!isKdfId(kdfId)) {
    throw new KdfError();
  }
  return kdfId;
}

function assertIvLength(iv: Uint8Array): void {
  if (iv.length !== IV_LENGTH) {
    throw new HeaderError(`iv must be ${IV_LENGTH} bytes`);
  }
}

function assertPolicyRange(policy: Policy): void {
  if (
    !isUnsignedInteger(policy.opensMax, MAX_OPENS) ||
    !isUnsignedInteger(policy.ttlSeconds, MAX_UINT32) ||
    !isUnsignedInteger(policy.deadline, MAX_UINT32)
  ) {
    throw new PolicyError('policy value out of range');
  }
}
