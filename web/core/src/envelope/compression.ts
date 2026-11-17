import { PlaintextError } from '../errors.ts';
import { MAX_FILE_BYTES } from '../limits.ts';
import { arrayBufferBacked } from '../primitives/bytes.ts';

const DECOMPRESS_CHUNK_BYTES = 1024;
const GZIP_HEADER_LENGTH = 10;
const GZIP_FLAGS_OFFSET = 3;
const GZIP_RESERVED_FLAGS = 0xe0;

export async function decompressAttachment(file: Uint8Array, size: number): Promise<Uint8Array<ArrayBuffer>> {
  if (
    !Number.isSafeInteger(size) ||
    size < 0 ||
    size > MAX_FILE_BYTES ||
    file.length < GZIP_HEADER_LENGTH ||
    ((file[GZIP_FLAGS_OFFSET] ?? 0) & GZIP_RESERVED_FLAGS) !== 0
  ) {
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
  return readExactly(source.pipeThrough(new DecompressionStream('gzip')), size);
}

async function readExactly(stream: ReadableStream<Uint8Array>, size: number): Promise<Uint8Array<ArrayBuffer>> {
  const reader = stream.getReader();
  const output = new Uint8Array(size);
  let written = 0;
  try {
    for (;;) {
      const chunk = await reader.read();
      if (chunk.done) {
        break;
      }
      const end = written + chunk.value.length;
      if (end > size) {
        chunk.value.fill(0);
        await reader.cancel();
        throw new PlaintextError();
      }
      output.set(chunk.value, written);
      chunk.value.fill(0);
      written = end;
    }
    if (written !== size) {
      throw new PlaintextError();
    }
    return output;
  } catch {
    output.fill(0);
    throw new PlaintextError();
  } finally {
    reader.releaseLock();
  }
}
