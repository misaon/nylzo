import { decode32, encodeBase64url } from './base64url.ts';
import { assertLength } from './bytes.ts';
import { KdfMismatchError, LinkError, LinkRetiredKdfError, LinkVersionError } from './errors.ts';
import { idToString, parseId } from './ids.ts';
import { KDF_RETIRED, type KdfId, parseKdfId } from './kdf.ts';
import { FRAGMENT_VERSION, SECRET_LEN } from './limits.ts';

export const FRAGMENT_LABEL = String(FRAGMENT_VERSION);
const LINK_PATH_PREFIX = '/m/';
const VERSION_LABEL = /^[1-9]\d{0,8}$/;

export interface Fragment {
  readonly kdfId: KdfId;
  readonly s: Uint8Array<ArrayBuffer>;
}

export interface Link extends Fragment {
  readonly id: Uint8Array<ArrayBuffer>;
}

export function fragment(kdfId: KdfId, s: Uint8Array): string {
  assertLength(s, SECRET_LEN, 'secret');
  return `${FRAGMENT_LABEL}.${String(kdfId)}.${encodeBase64url(s)}`;
}

export function parseFragment(text: string): Fragment {
  return parseFragmentBody(text.startsWith('#') ? text.slice(1) : text);
}

function newerFragment(text: string): boolean {
  const [label = ''] = text.split('.', 1);
  return VERSION_LABEL.test(label) && Number(label) > FRAGMENT_VERSION;
}

function parseFragmentBody(text: string): Fragment {
  if (newerFragment(text)) {
    throw new LinkVersionError();
  }
  const parts = text.split('.');
  const [label, kdfText = '', secretText = ''] = parts;
  if (label !== FRAGMENT_LABEL || parts.length !== 3) {
    throw new LinkError();
  }
  let s: Uint8Array<ArrayBuffer>;
  try {
    s = decode32(secretText);
  } catch {
    throw new LinkError();
  }
  if (kdfText === String(KDF_RETIRED)) {
    s.fill(0);
    throw new LinkRetiredKdfError();
  }
  try {
    return { kdfId: parseKdfId(kdfText), s };
  } catch {
    s.fill(0);
    throw new LinkError();
  }
}

export function formatLink(base: string, id: Uint8Array, kdfId: KdfId, s: Uint8Array): string {
  const origin = base.endsWith('/') ? base.slice(0, -1) : base;
  return `${origin}${LINK_PATH_PREFIX}${idToString(id)}#${fragment(kdfId, s)}`;
}

export function parsePath(path: string): Uint8Array<ArrayBuffer> {
  if (!path.startsWith(LINK_PATH_PREFIX)) {
    throw new LinkError();
  }
  try {
    return parseId(path.slice(LINK_PATH_PREFIX.length));
  } catch {
    throw new LinkError();
  }
}

const LINK_HEAD = /^https?:\/\/[\d.:[\]a-z-]+(\/[^?#]*)(?:\?[^#]*)?$/i;

export function parseLink(raw: string): Link {
  const hash = raw.indexOf('#');
  const path = hash === -1 ? undefined : LINK_HEAD.exec(raw.slice(0, hash))?.[1];
  if (path === undefined) {
    throw new LinkError();
  }
  const text = raw.slice(hash + 1);
  if (newerFragment(text)) {
    throw new LinkVersionError();
  }
  const id = parsePath(path);
  const { kdfId, s } = parseFragmentBody(text);
  return { id, kdfId, s };
}

export function assertProfile(fragmentKdfId: KdfId, serverKdfId: number): void {
  if (fragmentKdfId !== serverKdfId) {
    throw new KdfMismatchError();
  }
}
