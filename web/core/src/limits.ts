export const VERSION = 3;
export const FRAGMENT_VERSION = 3;

export const ID_LENGTH = 16;
export const SECRET_LENGTH = 32;
export const KEY_LENGTH = 32;
export const SEED_LENGTH = 32;
export const SHARE_LENGTH = 32;
export const PASSPHRASE_SALT_LENGTH = 32;
// Finalize prefixes the OPRF input with a two-byte length (RFC 9497 section 3.3.1).
export const MAX_OPRF_INPUT_LENGTH = 0xff_ff;
export const MAX_PASSPHRASE_BYTES = MAX_OPRF_INPUT_LENGTH - PASSPHRASE_SALT_LENGTH;
export const OBJECT_HASH_LENGTH = 32;
export const IV_LENGTH = 12;
export const TAG_LENGTH = 16;
export const HEADER_LENGTH = 15;
export const COMMITMENT_LENGTH = 32;
export const PREFIX_LENGTH = HEADER_LENGTH + COMMITMENT_LENGTH;
export const POLICY_LENGTH = 10;
// FLAG_DEADLINE says that the policy carries the deadline; every message created from Nylzo 0.14.0 on sets it.
export const FLAG_DEADLINE = 2;

export const MAX_TEXT_BYTES = 100 * 1024;
export const MAX_FILE_BYTES = 5 * 1024 * 1024;
export const MAX_META_BYTES = 102 * 1024;
export const META_LENGTH_PREFIX = 4;
export const PAD = 65_536;
const OBJECT_SLACK = 64;
export const MAX_OBJECT_BYTES = MAX_FILE_BYTES + MAX_TEXT_BYTES + PAD + OBJECT_SLACK;
export const MAX_FILE_NAME = 255;
export const MAX_MIME_BYTES = 255;
export const MAX_JSON_DEPTH = 32;

export const MIN_TTL_SECONDS = 60;

const MIN_TRANSFER_SECONDS = 120;
const MIN_TRANSFER_BYTES_PER_SECOND = 16_000;

export function isUnsignedInteger(value: number, maximum: number): boolean {
  return Number.isSafeInteger(value) && value >= 0 && value <= maximum;
}

export function transferSeconds(size: number): number {
  return Math.max(MIN_TRANSFER_SECONDS, Math.ceil(size / MIN_TRANSFER_BYTES_PER_SECOND));
}
