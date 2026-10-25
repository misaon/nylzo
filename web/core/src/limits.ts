export const VERSION = 1;
export const FRAGMENT_VERSION = 1;

export const ID_LEN = 16;
export const SECRET_LEN = 32;
export const KEY_LEN = 32;
export const TOKEN_LEN = 32;
export const IV_LEN = 12;
export const TAG_LEN = 16;
export const HEADER_LEN = 15;
export const POLICY_LEN = 6;
export const AAD_LEN = HEADER_LEN + ID_LEN + POLICY_LEN;

export const MAX_TEXT_BYTES = 100 * 1024;
export const MAX_FILE_BYTES = 5 * 1024 * 1024;
export const MAX_META_BYTES = 102 * 1024;
export const META_LEN_PREFIX = 4;
export const PAD_TEXT = 4096;
export const PAD_FILE = 65_536;
const OBJECT_SLACK = 64;
export const MAX_OBJECT_BYTES = MAX_FILE_BYTES + MAX_TEXT_BYTES + PAD_FILE + OBJECT_SLACK;
export const MAX_FILE_NAME = 255;
export const MAX_MIME = 255;
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
