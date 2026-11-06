import { takeHandedOverFragment } from '../local-client/handoff.ts';

declare global {
  var nylzoFragment: (() => string) | undefined;
}

function takeAddressFragment(): string {
  const handedOver = globalThis.nylzoFragment?.();
  if (handedOver !== undefined) {
    return handedOver;
  }
  const { hash, pathname, search } = location;
  if (hash !== '') {
    history.replaceState(null, '', `${pathname}${search}`);
  }
  return hash;
}

export function takeFragment(): { readonly fragment: string; readonly handedOver: boolean } {
  const fromLocalClient = takeHandedOverFragment();
  return fromLocalClient === undefined
    ? { fragment: takeAddressFragment(), handedOver: false }
    : { fragment: fromLocalClient, handedOver: true };
}
