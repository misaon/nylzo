import { EncodingError } from '../errors.ts';

const encoder = new TextEncoder();

export function utf8(text: string): Uint8Array<ArrayBuffer> {
  return encoder.encode(text);
}

export function concatenate(...parts: readonly Uint8Array[]): Uint8Array<ArrayBuffer> {
  const joined = new Uint8Array(parts.reduce((total, part) => total + part.length, 0));
  let offset = 0;
  for (const part of parts) {
    joined.set(part, offset);
    offset += part.length;
  }
  return joined;
}

export function xor(first: Uint8Array, second: Uint8Array): Uint8Array<ArrayBuffer> {
  const view = new DataView(second.buffer, second.byteOffset, second.byteLength);
  return Uint8Array.from(first, (byte, index) => byte ^ view.getUint8(index));
}

export function arrayBufferBacked(bytes: Uint8Array): Uint8Array<ArrayBuffer> {
  const { buffer } = bytes;
  return buffer instanceof ArrayBuffer
    ? new Uint8Array(buffer, bytes.byteOffset, bytes.byteLength)
    : new Uint8Array(bytes);
}

export function assertLength(bytes: Uint8Array, length: number, name: string): void {
  if (bytes.length !== length) {
    throw new EncodingError(`${name} must be ${length} bytes, got ${bytes.length}`);
  }
}
