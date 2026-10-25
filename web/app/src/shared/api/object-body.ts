import { MAX_OBJECT_BYTES } from '@nylzo/core';

import { ResponseError } from './api-errors.ts';

export type DownloadProgress = (received: number, total: number | undefined) => void;

const INITIAL_BYTES = 64 * 1024;

function tooLarge(): ResponseError {
  return new ResponseError('the object is larger than the protocol allows');
}

function grown(buffer: Uint8Array<ArrayBuffer>, used: number, needed: number): Uint8Array<ArrayBuffer> {
  if (needed <= buffer.length) {
    return buffer;
  }
  const larger = new Uint8Array(Math.min(MAX_OBJECT_BYTES, Math.max(needed, buffer.length * 2)));
  larger.set(buffer.subarray(0, used));
  return larger;
}

export async function readObject(response: Response, onProgress?: DownloadProgress): Promise<Uint8Array<ArrayBuffer>> {
  const declared = Number(response.headers.get('Content-Length'));
  const total = Number.isSafeInteger(declared) && declared > 0 ? declared : undefined;
  if (total !== undefined && total > MAX_OBJECT_BYTES) {
    await response.body?.cancel();
    throw tooLarge();
  }
  if (!response.body) {
    const whole = new Uint8Array(await response.arrayBuffer());
    if (whole.length > MAX_OBJECT_BYTES) {
      throw tooLarge();
    }
    return whole;
  }
  let object = new Uint8Array(total ?? INITIAL_BYTES);
  let received = 0;
  const reader = response.body.getReader();
  for (let chunk = await reader.read(); !chunk.done; chunk = await reader.read()) {
    if (received + chunk.value.length > MAX_OBJECT_BYTES) {
      await reader.cancel();
      throw tooLarge();
    }
    object = grown(object, received, received + chunk.value.length);
    object.set(chunk.value, received);
    received += chunk.value.length;
    onProgress?.(received, total);
  }
  return received === object.length ? object : object.slice(0, received);
}
