import { MAX_OBJECT_BYTES } from '@nylzo/core';

import { ResponseError } from './api-errors.ts';

export type DownloadProgress = (received: number, total: number | undefined) => void;

const INITIAL_BYTES = 64 * 1024;

function tooLarge(): ResponseError {
  return new ResponseError('the response is larger than the protocol allows');
}

function grown(buffer: Uint8Array<ArrayBuffer>, used: number, needed: number, limit: number): Uint8Array<ArrayBuffer> {
  if (needed <= buffer.length) {
    return buffer;
  }
  const larger = new Uint8Array(Math.min(limit, Math.max(needed, buffer.length * 2)));
  larger.set(buffer.subarray(0, used));
  return larger;
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
    if (received + chunk.value.length > limit) {
      await reader.cancel();
      throw tooLarge();
    }
    object = grown(object, received, received + chunk.value.length, limit);
    object.set(chunk.value, received);
    received += chunk.value.length;
    onProgress?.(received, total);
  }
  return received === object.length ? object : object.slice(0, received);
}

export async function readObject(response: Response, onProgress?: DownloadProgress): Promise<Uint8Array<ArrayBuffer>> {
  return readBody(response, MAX_OBJECT_BYTES, onProgress);
}
