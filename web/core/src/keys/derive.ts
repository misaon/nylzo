import { ID_LEN, KEY_LEN, SECRET_LEN, TOKEN_LEN } from '../limits.ts';
import { arrayBufferBacked, assertLength, concatenate, utf8 } from '../primitives/bytes.ts';
import { hash } from '../primitives/identifiers.ts';

const HKDF_SALT_LABEL = 'nylzo/v1/hkdf-salt';
const INFO_READ = 'nylzo/v1/read';
const INFO_PASSPHRASE_SALT = 'nylzo/v1/pw-salt';
const INFO_CONTENT = 'nylzo/v1/content';
const INFO_VERIFIER = 'nylzo/v1/verifier';
const PASSPHRASE_SALT_LEN = 32;

export interface LinkKeys {
  readonly r: Uint8Array<ArrayBuffer>;
  readonly saltPw: Uint8Array<ArrayBuffer>;
}

export interface ContentKeys {
  readonly k: CryptoKey;
  readonly v: Uint8Array<ArrayBuffer>;
}

export interface DerivedKeys {
  readonly link: LinkKeys;
  readonly content: ContentKeys;
}

export async function hsalt(id: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  assertLength(id, ID_LEN, 'id');
  return hash(concatenate(utf8(HKDF_SALT_LABEL), id));
}

export async function deriveLink(id: Uint8Array, secret: Uint8Array): Promise<LinkKeys> {
  assertLength(secret, SECRET_LEN, 'secret');
  const salt = await hsalt(id);
  const keyMaterial = await importKeyMaterial(arrayBufferBacked(secret));
  return {
    r: await deriveBytes(keyMaterial, salt, INFO_READ, TOKEN_LEN),
    saltPw: await deriveBytes(keyMaterial, salt, INFO_PASSPHRASE_SALT, PASSPHRASE_SALT_LEN),
  };
}

export async function deriveContent(
  id: Uint8Array,
  secret: Uint8Array,
  passphraseKey: Uint8Array,
): Promise<ContentKeys> {
  assertLength(secret, SECRET_LEN, 'secret');
  const salt = await hsalt(id);
  const secretAndPassphraseKey = concatenate(secret, passphraseKey);
  const keyMaterial = await importKeyMaterial(secretAndPassphraseKey);
  secretAndPassphraseKey.fill(0);
  const contentKey = await crypto.subtle.deriveKey(
    hkdfParameters(salt, INFO_CONTENT),
    keyMaterial,
    { name: 'AES-GCM', length: KEY_LEN * 8 },
    false,
    ['encrypt', 'decrypt'],
  );
  return { k: contentKey, v: await deriveBytes(keyMaterial, salt, INFO_VERIFIER, TOKEN_LEN) };
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
