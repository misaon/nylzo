import { arrayBufferBacked, assertLength, concat, utf8 } from './bytes.ts';
import { hash } from './ids.ts';
import { assertWellFormedPassphrase, derivePassphraseKey, type KdfId } from './kdf.ts';
import { ID_LEN, KEY_LEN, SECRET_LEN, TOKEN_LEN } from './limits.ts';

const HSALT_LABEL = 'nylzo/v1/hkdf-salt';
const INFO_READ = 'nylzo/v1/read';
const INFO_PW_SALT = 'nylzo/v1/pw-salt';
const INFO_CONTENT = 'nylzo/v1/content';
const INFO_VERIFIER = 'nylzo/v1/verifier';

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
  return hash(concat(utf8(HSALT_LABEL), id));
}

export async function deriveLink(id: Uint8Array, s: Uint8Array): Promise<LinkKeys> {
  assertLength(s, SECRET_LEN, 'secret');
  const salt = await hsalt(id);
  const prk = await importIkm(arrayBufferBacked(s));
  return {
    r: await expandBits(prk, salt, INFO_READ, TOKEN_LEN),
    saltPw: await expandBits(prk, salt, INFO_PW_SALT, 32),
  };
}

export async function deriveContent(id: Uint8Array, s: Uint8Array, p: Uint8Array): Promise<ContentKeys> {
  assertLength(s, SECRET_LEN, 'secret');
  const salt = await hsalt(id);
  const ikm = concat(s, p);
  const prk = await importIkm(ikm);
  ikm.fill(0);
  const k = await crypto.subtle.deriveKey(
    hkdfParams(salt, INFO_CONTENT),
    prk,
    { name: 'AES-GCM', length: KEY_LEN * 8 },
    false,
    ['encrypt', 'decrypt'],
  );
  return { k, v: await expandBits(prk, salt, INFO_VERIFIER, TOKEN_LEN) };
}

export async function deriveAll(
  id: Uint8Array,
  s: Uint8Array,
  kdfId: KdfId,
  passphrase: string,
): Promise<DerivedKeys> {
  assertWellFormedPassphrase(kdfId, passphrase);
  const link = await deriveLink(id, s);
  const p = await derivePassphraseKey(kdfId, passphrase, link.saltPw);
  const content = await deriveContent(id, s, p);
  return { link, content };
}

function importIkm(ikm: Uint8Array<ArrayBuffer>): Promise<CryptoKey> {
  return crypto.subtle.importKey('raw', ikm, 'HKDF', false, ['deriveBits', 'deriveKey']);
}

function hkdfParams(salt: Uint8Array<ArrayBuffer>, info: string): HkdfParams {
  return { name: 'HKDF', hash: 'SHA-256', salt, info: utf8(info) };
}

async function expandBits(
  prk: CryptoKey,
  salt: Uint8Array<ArrayBuffer>,
  info: string,
  length: number,
): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await crypto.subtle.deriveBits(hkdfParams(salt, info), prk, length * 8));
}
