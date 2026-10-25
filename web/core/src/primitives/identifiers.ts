import { ID_LENGTH, SECRET_LENGTH } from '../limits.ts';
import { decodeBase64urlExact, encodeBase64url } from './base64url.ts';
import { arrayBufferBacked, assertLength } from './bytes.ts';

export function parseId(text: string): Uint8Array<ArrayBuffer> {
  return decodeBase64urlExact(text, ID_LENGTH);
}

export function idToString(id: Uint8Array): string {
  assertLength(id, ID_LENGTH, 'id');
  return encodeBase64url(id);
}

export function newSecret(): Uint8Array<ArrayBuffer> {
  return crypto.getRandomValues(new Uint8Array(SECRET_LENGTH));
}

export async function hash(bytes: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await crypto.subtle.digest('SHA-256', arrayBufferBacked(bytes)));
}
