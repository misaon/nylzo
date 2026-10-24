const EMAIL = /^[A-Za-z0-9_%+-]+(\.[A-Za-z0-9_%+-]+)*@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$/u;
const HTTPS = /^https:\/\/[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*(:[0-9]{1,5})?(\/[A-Za-z0-9._~:/?#[\]@!$&'()*+,;=%-]*)?$/u;

export function contactHref(contact: string): string | undefined {
  if (EMAIL.test(contact)) {
    return `mailto:${contact}`;
  }
  return HTTPS.test(contact) ? contact : undefined;
}

export function verifyCommand(origin: string): string {
  return `nylzo verify-client --server ${origin}`;
}
