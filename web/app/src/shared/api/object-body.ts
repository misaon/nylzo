import { MAX_OBJECT_BYTES } from '@nylzo/core';

import { ResponseError } from './api-errors.ts';

export type DownloadProgress = (received: number, total: number | undefined) => void;

const INITIAL_BYTES = 64 * 1024;

function tooLarge(): ResponseError {
  return new ResponseError('the response is larger than the protocol allows');
}

export async function readBody(
  response: Response,
  limit: number,
  onProgress?: DownloadProgress,
): Promise<Uint8Array<ArrayBuffer>> {
  const declared = Number(response.headers.get('Content-Length'));
  const total = Number.isSafeInteger(declared) && declared > 0 ? declared : undefined;
  if (total !== undefined && total > limit) {
    await response.body?.cancel();
    throw tooLarge();
  }
  if (!response.body) {
    const whole = new Uint8Array(await response.arrayBuffer());
    if (whole.length > limit) {
      throw tooLarge();
    }
    return whole;
  }
  let object = new Uint8Array(total ?? Math.min(INITIAL_BYTES, limit));
  let received = 0;
  const reader = response.body.getReader();
  for (let chunk = await reader.read(); !chunk.done; chunk = await reader.read()) {
    const needed = received + chunk.value.length;
    if (needed > limit) {
      await reader.cancel();
      throw tooLarge();
    }
    if (needed > object.length) {
      const capacity = Math.min(limit, Math.max(needed, object.length * 2));
      object = new Uint8Array(object.buffer.transfer(capacity));
    }
    object.set(chunk.value, received);
    received = needed;
    onProgress?.(received, total);
  }
  return received === object.length ? object : new Uint8Array(object.buffer.transfer(received));
}

export function mediaTypeOf(response: Response): string {
  return (response.headers.get('Content-Type') ?? '').split(';', 1)[0]?.trim().toLowerCase() ?? '';
}

export async function readObject(response: Response, onProgress?: DownloadProgress): Promise<Uint8Array<ArrayBuffer>> {
  if (mediaTypeOf(response) !== 'application/octet-stream') {
    await response.body?.cancel();
    throw new ResponseError('the object is not application/octet-stream');
  }
  return readBody(response, MAX_OBJECT_BYTES, onProgress);
}
