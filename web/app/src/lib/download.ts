const INVISIBLE = /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}ᅟᅠㅤﾠ]/gu;
const RESERVED = /[/\\:*?"<>|]/g;
const WINDOWS_DEVICE = /^(con|prn|aux|nul|com[0-9¹²³]|lpt[0-9¹²³]|conin\$|conout\$)\s*(\.|$)/iu;
const MAX_NAME_BYTES = 255;
const MAX_EXTENSION_CHARS = 16;
const FALLBACK_NAME = 'nylzo';

const SAFE_EXTENSIONS = new Set([
  'txt', 'md', 'csv', 'log', 'json',
  'pdf',
  'png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'heic', 'heif', 'bmp', 'tif', 'tiff',
  'docx', 'xlsx', 'pptx', 'odt', 'ods', 'odp',
]);

const encoder = new TextEncoder();

function byteLength(text: string): number {
  return encoder.encode(text).length;
}

function truncateBytes(text: string, max: number): string {
  let out = '';
  let used = 0;
  for (const char of text) {
    const size = byteLength(char);
    if (used + size > max) {
      break;
    }
    out += char;
    used += size;
  }
  return out;
}

export function sanitizeFileName(name: string): string {
  let clean = name.normalize('NFC').replace(INVISIBLE, '').replace(RESERVED, '_').replace(/\s+/gu, ' ').trim();
  clean = clean.replace(/^[.\s]+/u, '').replace(/[.\s]+$/u, '');
  if (WINDOWS_DEVICE.test(clean)) {
    clean = `_${clean}`;
  }
  if (clean === '') {
    return FALLBACK_NAME;
  }
  if (byteLength(clean) <= MAX_NAME_BYTES) {
    return clean;
  }
  const dot = clean.lastIndexOf('.');
  const extension = dot > 0 && clean.length - dot - 1 <= MAX_EXTENSION_CHARS ? clean.slice(dot) : '';
  const stem = extension ? clean.slice(0, dot) : clean;
  return truncateBytes(stem, MAX_NAME_BYTES - byteLength(extension)) + extension;
}

export function fileExtension(name: string): string {
  const clean = sanitizeFileName(name);
  const dot = clean.lastIndexOf('.');
  return dot > 0 ? clean.slice(dot + 1).toLowerCase() : '';
}

export function isKnownSafeFile(name: string): boolean {
  return SAFE_EXTENSIONS.has(fileExtension(name));
}

export function saveFile(bytes: Uint8Array<ArrayBuffer>, name: string): void {
  const url = URL.createObjectURL(new Blob([bytes], { type: 'application/octet-stream' }));
  const link = document.createElement('a');
  link.href = url;
  link.download = sanitizeFileName(name);
  link.rel = 'noopener';
  link.hidden = true;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}
