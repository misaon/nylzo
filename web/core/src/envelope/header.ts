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
  // When the message expires, in Unix seconds; absent for a message created before Nylzo 0.14.0.
  readonly deadline?: number;
}

const IV_OFFSET = 3;
// LEGACY_POLICY_LENGTH is the policy of a message created before Nylzo 0.14.0, without the deadline.
const LEGACY_POLICY_LENGTH = 6;
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
  if (policy.opensMax === 0 || policy.ttlSeconds < MIN_TTL_SECONDS) {
    throw new PolicyError();
  }
}

// policyFits reports whether the policy carries a deadline exactly when the header says it does.
export function policyFits(header: Header, policy: Policy): boolean {
  return ((header.flags & FLAG_DEADLINE) !== 0) === (policy.deadline !== undefined);
}

// policyBytes encodes the policy of an object with its header, which tells whether the deadline belongs to it.
export function policyBytes(header: Header, policy: Policy): Uint8Array<ArrayBuffer> {
  assertPolicyRange(policy);
  if (!policyFits(header, policy)) {
    throw new PolicyError('the deadline does not match the header');
  }
  const withDeadline = policy.deadline !== undefined;
  const bytes = new Uint8Array(withDeadline ? POLICY_LENGTH : LEGACY_POLICY_LENGTH);
  const view = new DataView(bytes.buffer);
  view.setUint16(0, policy.opensMax);
  view.setUint32(2, policy.ttlSeconds);
  if (withDeadline) {
    view.setUint32(LEGACY_POLICY_LENGTH, policy.deadline);
  }
  return bytes;
}

export function aad(header: Header, commitment: Uint8Array, id: Uint8Array, policy: Policy): Uint8Array<ArrayBuffer> {
  assertLength(id, ID_LENGTH, 'id');
  return concatenate(headerBytes(header), commitment, id, policyBytes(header, policy));
}

function checkHeaderFields(version: number, kdfId: number, flags: number): KdfId {
  if (version !== VERSION) {
    throw new VersionError();
  }
  if (flags !== FLAG_DEADLINE && flags !== 0) {
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
  const deadlineInRange =
    policy.deadline === undefined || (isUnsignedInteger(policy.deadline, MAX_UINT32) && policy.deadline > 0);
  if (
    !isUnsignedInteger(policy.opensMax, MAX_OPENS) ||
    !isUnsignedInteger(policy.ttlSeconds, MAX_UINT32) ||
    !deadlineInRange
  ) {
    throw new PolicyError('policy value out of range');
  }
}
