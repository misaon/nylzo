import { EncodingError } from '../errors.ts';

const URL_ALPHABET = /^[\w-]*$/u;

function encodedLength(byteLength: number): number {
  return Math.ceil((byteLength * 4) / 3);
}

export function encodeBase64url(bytes: Uint8Array): string {
  return bytes.toBase64({ alphabet: 'base64url', omitPadding: true });
}

export function decodeBase64url(text: string): Uint8Array<ArrayBuffer> {
  return decodeBase64urlExact(text, Math.floor((text.length * 3) / 4));
}

// fromBase64 skips ASCII whitespace and has no strict unpadded mode, so the alphabet check and the padding come first.
export function decodeBase64urlExact(text: string, byteLength: number): Uint8Array<ArrayBuffer> {
  if (text.length !== encodedLength(byteLength) || !URL_ALPHABET.test(text)) {
    throw new EncodingError();
  }
  try {
    return Uint8Array.fromBase64(text.padEnd(Math.ceil(text.length / 4) * 4, '='), {
      alphabet: 'base64url',
      lastChunkHandling: 'strict',
    });
  } catch {
    throw new EncodingError();
  }
}
