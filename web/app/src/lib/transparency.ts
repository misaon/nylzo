const EMAIL = /^[^\s@<>()[\]\\,;:"?#/]+@[^\s@<>()[\]\\,;:"?#/]+\.[^\s@<>()[\]\\,;:"?#/.]+$/u;

export function contactHref(contact: string): string | undefined {
  if (EMAIL.test(contact)) {
    return `mailto:${contact}`;
  }
  if (!contact.startsWith('https://') || /[\s<>"\\]/u.test(contact)) {
    return undefined;
  }
  try {
    const url = new URL(contact);
    return url.hostname !== '' && url.username === '' && url.password === '' ? url.href : undefined;
  } catch {
    return undefined;
  }
}

export function verifyCommand(origin: string): string {
  return `nylzo verify-client --server ${origin}`;
}
