import { EncodingError } from './errors.ts';

const encoder = new TextEncoder();

export function utf8(text: string): Uint8Array<ArrayBuffer> {
  return encoder.encode(text);
}

export function concat(...parts: readonly Uint8Array[]): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(parts.reduce((total, part) => total + part.length, 0));
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}

export function arrayBufferBacked(bytes: Uint8Array): Uint8Array<ArrayBuffer> {
  const { buffer } = bytes;
  if (buffer instanceof ArrayBuffer) {
    return new Uint8Array(buffer, bytes.byteOffset, bytes.byteLength);
  }
  return bytes.slice();
}

export function assertLength(bytes: Uint8Array, length: number, what: string): void {
  if (bytes.length !== length) {
    throw new EncodingError(`${what} must be ${length} bytes, got ${bytes.length}`);
  }
}
