import { EncodingError } from './errors.ts';

const ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';
const SIX_BITS = 0b11_1111;
const EIGHT_BITS = 0b1111_1111;

export function encodedLength(byteLength: number): number {
  return Math.ceil((byteLength * 4) / 3);
}

export function encodeBase64url(bytes: Uint8Array): string {
  let out = '';
  let accumulator = 0;
  let bits = 0;
  for (const byte of bytes) {
    accumulator = ((accumulator & SIX_BITS) << 8) | byte;
    bits += 8;
    while (bits >= 6) {
      bits -= 6;
      out += ALPHABET.charAt((accumulator >> bits) & SIX_BITS);
    }
  }
  if (bits > 0) {
    out += ALPHABET.charAt((accumulator << (6 - bits)) & SIX_BITS);
  }
  return out;
}

export function decodeBase64urlExact(text: string, byteLength: number): Uint8Array<ArrayBuffer> {
  if (text.length !== encodedLength(byteLength)) {
    throw new EncodingError();
  }
  const out = new Uint8Array(byteLength);
  let accumulator = 0;
  let bits = 0;
  let written = 0;
  for (const character of text) {
    const value = ALPHABET.indexOf(character);
    if (value === -1) {
      throw new EncodingError();
    }
    accumulator = ((accumulator & EIGHT_BITS) << 6) | value;
    bits += 6;
    if (bits < 8) {
      continue;
    }
    bits -= 8;
    out[written++] = (accumulator >> bits) & EIGHT_BITS;
  }
  if (written !== byteLength || (accumulator & ((1 << bits) - 1)) !== 0) {
    throw new EncodingError();
  }
  return out;
}

export function decode32(text: string): Uint8Array<ArrayBuffer> {
  return decodeBase64urlExact(text, 32);
}
