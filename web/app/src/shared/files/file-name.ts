const INVISIBLE = /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}\u{115F}\u{1160}\u{3164}\u{FFA0}]/gu;
const RESERVED = /[/\\:*?"<>|]/g;
const WINDOWS_DEVICE = /^(?:con|prn|aux|nul|com[\d¹²³]|lpt[\d¹²³]|conin\$|conout\$)\s*(?:\.|$)/iu;
const MAX_NAME_BYTES = 255;
const MAX_EXTENSION_BYTES = 16;
const FALLBACK_NAME = 'nylzo';

const SAFE_EXTENSIONS = new Set([
  'txt',
  'md',
  'csv',
  'log',
  'json',
  'pdf',
  'png',
  'jpg',
  'jpeg',
  'gif',
  'webp',
  'avif',
  'heic',
  'heif',
  'bmp',
  'tif',
  'tiff',
  'docx',
  'xlsx',
  'pptx',
  'odt',
  'ods',
  'odp',
]);

const encoder = new TextEncoder();

function byteLength(text: string): number {
  return encoder.encode(text).length;
}

function truncateBytes(text: string, maxBytes: number): string {
  let truncated = '';
  let usedBytes = 0;
  for (const character of text) {
    const size = byteLength(character);
    if (usedBytes + size > maxBytes) {
      break;
    }
    truncated += character;
    usedBytes += size;
  }
  return truncated;
}

export function splitFileName(name: string): { readonly stem: string; readonly extension: string } {
  const dot = name.lastIndexOf('.');
  return dot > 0 && byteLength(name.slice(dot + 1)) <= MAX_EXTENSION_BYTES
    ? { stem: name.slice(0, dot), extension: name.slice(dot) }
    : { stem: name, extension: '' };
}

export function sanitizeFileName(name: string): string {
  let clean = name
    .normalize('NFC')
    .replaceAll(INVISIBLE, '')
    .replaceAll(RESERVED, '_')
    .replaceAll(/\s+/gu, ' ')
    .trim()
    .replace(/^[.\s]+/u, '')
    .replace(/[.\s]+$/u, '');
  if (WINDOWS_DEVICE.test(clean)) {
    clean = `_${clean}`;
  }
  if (clean === '') {
    return FALLBACK_NAME;
  }
  if (byteLength(clean) <= MAX_NAME_BYTES) {
    return clean;
  }
  const { stem, extension } = splitFileName(clean);
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
