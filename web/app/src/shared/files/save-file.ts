import { sanitizeFileName } from './file-name.ts';

const REVOKE_AFTER_MS = 60_000;

const savedFiles = new Map<string, ReturnType<typeof setTimeout>>();

function revoke(url: string): void {
  clearTimeout(savedFiles.get(url));
  savedFiles.delete(url);
  URL.revokeObjectURL(url);
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
  savedFiles.set(
    url,
    setTimeout(() => revoke(url), REVOKE_AFTER_MS),
  );
}

// A frozen page runs no timers, so leaving it must release the decrypted bytes itself.
export function revokeSavedFiles(): void {
  for (const url of savedFiles.keys()) {
    revoke(url);
  }
}
