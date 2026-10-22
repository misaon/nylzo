import { decodeBase64urlExact, encodeBase64url } from './base64url.ts';
import { arrayBufferBacked, assertLength } from './bytes.ts';
import { ID_LEN, SECRET_LEN } from './limits.ts';

export function newId(): Uint8Array<ArrayBuffer> {
  return crypto.getRandomValues(new Uint8Array(ID_LEN));
}

export function parseId(text: string): Uint8Array<ArrayBuffer> {
  return decodeBase64urlExact(text, ID_LEN);
}

export function idToString(id: Uint8Array): string {
  assertLength(id, ID_LEN, 'id');
  return encodeBase64url(id);
}

export function newSecret(): Uint8Array<ArrayBuffer> {
  return crypto.getRandomValues(new Uint8Array(SECRET_LEN));
}

export async function hash(bytes: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await crypto.subtle.digest('SHA-256', arrayBufferBacked(bytes)));
}
