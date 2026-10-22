import { EncodingError } from './errors.ts';

const ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';

const VALUES = new Int8Array(128).fill(-1);
for (let i = 0; i < ALPHABET.length; i++) {
  VALUES[ALPHABET.charCodeAt(i)] = i;
}

export function encodedLength(byteLength: number): number {
  return Math.ceil((byteLength * 4) / 3);
}

export function encodeBase64url(bytes: Uint8Array): string {
  let out = '';
  let acc = 0;
  let bits = 0;
  for (const byte of bytes) {
    acc = ((acc & 0x3f) << 8) | byte;
    bits += 8;
    while (bits >= 6) {
      bits -= 6;
      out += ALPHABET.charAt((acc >> bits) & 0x3f);
    }
  }
  if (bits > 0) {
    out += ALPHABET.charAt((acc << (6 - bits)) & 0x3f);
  }
  return out;
}

export function decodeBase64urlExact(text: string, byteLength: number): Uint8Array<ArrayBuffer> {
  if (text.length !== encodedLength(byteLength)) {
    throw new EncodingError();
  }
  const out = new Uint8Array(byteLength);
  let acc = 0;
  let bits = 0;
  let written = 0;
  for (let i = 0; i < text.length; i++) {
    const value = valueOf(text.charCodeAt(i));
    if (value < 0) {
      throw new EncodingError();
    }
    acc = ((acc & 0xff) << 6) | value;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out[written++] = (acc >> bits) & 0xff;
    }
  }
  if (written !== byteLength || (acc & ((1 << bits) - 1)) !== 0) {
    throw new EncodingError();
  }
  return out;
}

export function decode32(text: string): Uint8Array<ArrayBuffer> {
  return decodeBase64urlExact(text, 32);
}

function valueOf(code: number): number {
  if (code >= VALUES.length) {
    return -1;
  }
  return VALUES[code] ?? -1;
}
