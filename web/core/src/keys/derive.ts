import { ShareError } from '../errors.ts';
import { ID_LENGTH, KEY_LENGTH, PASSPHRASE_SALT_LENGTH, SECRET_LENGTH, SEED_LENGTH, SHARE_LENGTH } from '../limits.ts';
import { arrayBufferBacked, assertLength, concatenate, utf8 } from '../primitives/bytes.ts';
import { hash } from '../primitives/identifiers.ts';

const EXPORT_KEY_LENGTH = 64;
const LABEL_HSALT = 'nylzo/v3/hkdf-salt';
const LABEL_READ = 'nylzo/v3/read';
const LABEL_PASSPHRASE_SALT = 'nylzo/v3/pw-salt';
const LABEL_CONTENT = 'nylzo/v3/content';
const LABEL_COMMITMENT = 'nylzo/v3/commitment';
const NO_SALT = new Uint8Array(0);

export interface LinkKeys {
  readonly readSeed: Uint8Array<ArrayBuffer>;
  readonly passphraseSalt: Uint8Array<ArrayBuffer>;
}

export interface ContentKeys {
  readonly contentKey: CryptoKey;
  readonly commitmentKey: CryptoKey;
}

export async function hsalt(id: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  assertLength(id, ID_LENGTH, 'id');
  return hash(concatenate(utf8(LABEL_HSALT), id));
}

// salt_pw comes from S alone: a sender sends its registration request before the server has issued the id.
export async function deriveLink(id: Uint8Array, secret: Uint8Array): Promise<LinkKeys> {
  assertLength(secret, SECRET_LENGTH, 'secret');
  const salt = await hsalt(id);
  const keyMaterial = await importKeyMaterial(arrayBufferBacked(secret));
  const readSeed = await deriveBytes(keyMaterial, salt, LABEL_READ, SEED_LENGTH);
  try {
    return {
      readSeed,
      passphraseSalt: await deriveBytes(keyMaterial, NO_SALT, LABEL_PASSPHRASE_SALT, PASSPHRASE_SALT_LENGTH),
    };
  } catch (error) {
    readSeed.fill(0);
    throw error;
  }
}

export async function derivePassphraseSalt(secret: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  assertLength(secret, SECRET_LENGTH, 'secret');
  const keyMaterial = await importKeyMaterial(arrayBufferBacked(secret));
  return deriveBytes(keyMaterial, NO_SALT, LABEL_PASSPHRASE_SALT, PASSPHRASE_SALT_LENGTH);
}

// P is the export_key of OPAQUE (spec section 2.1), for a message with or without a passphrase.
export async function deriveContent(
  id: Uint8Array,
  secret: Uint8Array,
  exportKey: Uint8Array,
  share: Uint8Array | undefined,
): Promise<ContentKeys> {
  assertLength(secret, SECRET_LENGTH, 'secret');
  assertLength(exportKey, EXPORT_KEY_LENGTH, 'export key');
  if (share === undefined) {
    throw new ShareError();
  }
  assertLength(share, SHARE_LENGTH, 'share');
  const salt = await hsalt(id);
  const keyMaterial = await importWiped(concatenate(secret, exportKey, share));
  const contentKey = await crypto.subtle.deriveKey(
    hkdfParameters(salt, LABEL_CONTENT),
    keyMaterial,
    { name: 'AES-GCM', length: KEY_LENGTH * 8 },
    false,
    ['encrypt', 'decrypt'],
  );
  const commitmentKey = await crypto.subtle.deriveKey(
    hkdfParameters(salt, LABEL_COMMITMENT),
    keyMaterial,
    { name: 'HMAC', hash: 'SHA-256', length: KEY_LENGTH * 8 },
    false,
    ['sign', 'verify'],
  );
  return { contentKey, commitmentKey };
}

async function importWiped(bytes: Uint8Array<ArrayBuffer>): Promise<CryptoKey> {
  try {
    return await importKeyMaterial(bytes);
  } finally {
    bytes.fill(0);
  }
}

function importKeyMaterial(bytes: Uint8Array<ArrayBuffer>): Promise<CryptoKey> {
  return crypto.subtle.importKey('raw', bytes, 'HKDF', false, ['deriveBits', 'deriveKey']);
}

function hkdfParameters(salt: Uint8Array<ArrayBuffer>, info: string): HkdfParams {
  return { name: 'HKDF', hash: 'SHA-256', salt, info: utf8(info) };
}

async function deriveBytes(
  keyMaterial: CryptoKey,
  salt: Uint8Array<ArrayBuffer>,
  info: string,
  length: number,
): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await crypto.subtle.deriveBits(hkdfParameters(salt, info), keyMaterial, length * 8));
}
