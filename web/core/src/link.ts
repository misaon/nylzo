import { decode32, encodeBase64url } from './base64url.ts';
import { assertLength } from './bytes.ts';
import { KdfMismatchError, LinkError } from './errors.ts';
import { idToString, parseId } from './ids.ts';
import { parseKdfId, type KdfId } from './kdf.ts';
import { SECRET_LEN } from './limits.ts';

export const FRAGMENT_LABEL = '1';
const LINK_PATH_PREFIX = '/m/';

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
  const parts = (text.startsWith('#') ? text.slice(1) : text).split('.');
  const [label, kdfText = '', secretText = ''] = parts;
  if (parts.length !== 3 || label !== FRAGMENT_LABEL) {
    throw new LinkError();
  }
  try {
    return { kdfId: parseKdfId(kdfText), s: decode32(secretText) };
  } catch {
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

const LINK_HEAD = /^[Hh][Tt][Tt][Pp][Ss]?:\/\/[A-Za-z0-9.:[\]-]+(\/[^?#]*)(\?[^#]*)?$/;

export function parseLink(raw: string): Link {
  const hash = raw.indexOf('#');
  const path = hash < 0 ? undefined : LINK_HEAD.exec(raw.slice(0, hash))?.[1];
  if (path === undefined) {
    throw new LinkError();
  }
  const id = parsePath(path);
  const { kdfId, s } = parseFragment(raw.slice(hash + 1));
  return { id, kdfId, s };
}

export function assertProfile(fragmentKdfId: KdfId, serverKdfId: number): void {
  if (fragmentKdfId !== serverKdfId) {
    throw new KdfMismatchError();
  }
}
