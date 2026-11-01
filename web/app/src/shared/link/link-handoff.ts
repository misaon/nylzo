import { idToString, LinkRetiredKdfError, LinkVersionError, parseLink } from '@nylzo/core';

import { takeHandedOverFragment } from '../local-client/handoff.ts';

export type LinkProblem = 'foreign' | 'invalid' | 'newer' | 'retired';

export type HandOver = { readonly path: string } | { readonly problem: LinkProblem };

declare global {
  var nylzoFragment: (() => string) | undefined;
}

const LINK_ORIGIN = /^(https?:\/\/[^/?#]+)/i;

const pasted: { link?: { readonly path: string; readonly fragment: string } } = {};

function problemOf(error: unknown): LinkProblem {
  if (error instanceof LinkVersionError) {
    return 'newer';
  }
  return error instanceof LinkRetiredKdfError ? 'retired' : 'invalid';
}

export function handOverLink(text: string, origin: string): HandOver {
  const link = text.trim();
  let path: string;
  try {
    const parsed = parseLink(link);
    parsed.secret.fill(0);
    path = `/m/${idToString(parsed.id)}`;
  } catch (error) {
    return { problem: problemOf(error) };
  }
  if (LINK_ORIGIN.exec(link)?.[1]?.toLowerCase() !== origin.toLowerCase()) {
    return { problem: 'foreign' };
  }
  pasted.link = { path, fragment: link.slice(link.indexOf('#')) };
  return { path };
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

export function takeFragment(path: string): { readonly fragment: string; readonly pasted: boolean } {
  const handedOver = pasted.link;
  delete pasted.link;
  if (handedOver?.path === path) {
    return { fragment: handedOver.fragment, pasted: true };
  }
  const fromLocalClient = takeHandedOverFragment();
  return fromLocalClient === undefined
    ? { fragment: takeAddressFragment(), pasted: false }
    : { fragment: fromLocalClient, pasted: true };
}
