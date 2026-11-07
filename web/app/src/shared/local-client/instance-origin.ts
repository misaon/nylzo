const INSTANCE = 'meta[name="nylzo-instance"]';
const ORIGIN =
  /^(?:https:\/\/(?:[a-z0-9.-]+|\[[0-9a-f:.]+\])|http:\/\/(?:localhost|127(?:\.\d{1,3}){3}|\[::1\]))(?::\d{1,5})?$/u;

// The verified local client runs on loopback, so its links must name the instance it relays to, never itself.
export function instanceOrigin(): string {
  const configured = document.querySelector<HTMLMetaElement>(INSTANCE)?.content ?? '';
  return ORIGIN.test(configured) ? configured : location.origin;
}

// Each session of the local client has a new origin that nobody purges later, so it keeps no key in the browser.
// Origins are compared after parsing, so an instance configured as https://host:443 is still itself.
export function servedByLocalClient(): boolean {
  return new URL(instanceOrigin()).origin !== location.origin;
}
