const EMAIL = /^[\w%+-]+(?:\.[\w%+-]+)*@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+$/u;
const HTTPS = /^https:\/\/[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*(?::\d{1,5})?(?:\/[\w.~:/?#[\]@!$&'()*+,;=%-]*)?$/u;

export function contactHref(contact: string): string | undefined {
  if (EMAIL.test(contact)) {
    return `mailto:${contact}`;
  }
  return HTTPS.test(contact) ? contact : undefined;
}
