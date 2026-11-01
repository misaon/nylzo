const INSTANCE = 'meta[name="nylzo-instance"]';
const ORIGIN =
  /^(?:https:\/\/(?:[a-z0-9.-]+|\[[0-9a-f:.]+\])|http:\/\/(?:localhost|127(?:\.\d{1,3}){3}|\[::1\]))(?::\d{1,5})?$/;

// The verified local client runs on loopback, so its links must name the instance it relays to, never itself.
export function instanceOrigin(): string {
  const configured = document.querySelector<HTMLMetaElement>(INSTANCE)?.content ?? '';
  return ORIGIN.test(configured) ? configured : location.origin;
}

// Each session of the local client has a new origin that nobody purges later, so it keeps no key in the browser.
export function servedByLocalClient(): boolean {
  return instanceOrigin() !== location.origin;
}
