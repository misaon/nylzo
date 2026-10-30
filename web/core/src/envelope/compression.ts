import { PlaintextError } from '../errors.ts';
import { MAX_FILE_BYTES } from '../limits.ts';
import { arrayBufferBacked } from '../primitives/bytes.ts';

const MIN_COMPRESS_BYTES = 128 * 1024;
const DECOMPRESS_CHUNK_BYTES = 1024;

export async function compressAttachment(file: Uint8Array): Promise<Uint8Array<ArrayBuffer> | undefined> {
  if (typeof CompressionStream === 'undefined' || file.length < MIN_COMPRESS_BYTES) {
    return undefined;
  }
  const source = new Blob([arrayBufferBacked(file)]).stream();
  // A new context for this file; message text and metadata never enter it.
  const compressed = source.pipeThrough(new CompressionStream('gzip'));
  return readBounded(compressed, file.length);
}

export async function decompressAttachment(file: Uint8Array, size: number): Promise<Uint8Array<ArrayBuffer>> {
  if (!Number.isSafeInteger(size) || size < 0 || size > MAX_FILE_BYTES) {
    throw new PlaintextError();
  }
  // WebKit buffers a whole input chunk's expansion before handing it back.
  // Small chunks bound that work before the output limit can cancel the stream.
  let offset = 0;
  const source = new ReadableStream<BufferSource>({
    pull(controller) {
      if (offset === file.length) {
        controller.close();
        return;
      }
      const chunk = file.subarray(offset, offset + DECOMPRESS_CHUNK_BYTES);
      offset += chunk.length;
      controller.enqueue(arrayBufferBacked(chunk));
    },
  });
  const stream = source.pipeThrough(new DecompressionStream('gzip'));
  const output = await readBounded(stream, size);
  if (output?.length !== size) {
    output?.fill(0);
    throw new PlaintextError();
  }
  return output;
}

async function readBounded(
  stream: ReadableStream<Uint8Array>,
  limit: number,
): Promise<Uint8Array<ArrayBuffer> | undefined> {
  const reader = stream.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const chunk = await reader.read();
      if (chunk.done) {
        break;
      }
      size += chunk.value.length;
      if (size > limit) {
        chunk.value.fill(0);
        await reader.cancel();
        return undefined;
      }
      chunks.push(chunk.value);
    }
    const output = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) {
      output.set(chunk, offset);
      offset += chunk.length;
    }
    return output;
  } catch {
    throw new PlaintextError();
  } finally {
    for (const chunk of chunks) {
      chunk.fill(0);
    }
    reader.releaseLock();
  }
}
