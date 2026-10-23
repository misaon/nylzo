const CONTROL = /[\u0000-\u001f\u007f-\u009f]/g;
const INVISIBLE = /[­؜᠎​-‏‪-‮⁠-⁤⁦-⁩﻿]/g;
const RESERVED = /[/\\:*?"<>|]/g;
const WINDOWS_DEVICE = /^(con|prn|aux|nul|com[0-9]|lpt[0-9])(\.|$)/i;
const MAX_NAME_BYTES = 255;
const MAX_EXTENSION_CHARS = 16;
const FALLBACK_NAME = 'nylzo';

const RISKY_EXTENSIONS = new Set([
  'apk', 'app', 'bat', 'cmd', 'com', 'cpl', 'dll', 'dmg', 'exe', 'hta', 'htm', 'html', 'iso', 'jar', 'js', 'jse',
  'lnk', 'msi', 'msix', 'pkg', 'ps1', 'reg', 'scr', 'sh', 'svg', 'url', 'vbe', 'vbs', 'wsf', 'xhtml',
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
  let clean = name.normalize('NFC').replace(CONTROL, '').replace(INVISIBLE, '').replace(RESERVED, '_').trim();
  clean = clean.replace(/^[.\s]+/, '').replace(/[.\s]+$/, '');
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

export function isRiskyFile(name: string): boolean {
  const clean = sanitizeFileName(name).toLowerCase();
  const dot = clean.lastIndexOf('.');
  return dot >= 0 && RISKY_EXTENSIONS.has(clean.slice(dot + 1));
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
