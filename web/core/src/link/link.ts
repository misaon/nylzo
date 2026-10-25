import { KdfMismatchError, LinkError, LinkRetiredKdfError, LinkVersionError } from '../errors.ts';
import { KDF_RETIRED, type KdfId, parseKdfId } from '../keys/kdf.ts';
import { FRAGMENT_VERSION, SECRET_LENGTH } from '../limits.ts';
import { decodeBase64urlExact, encodeBase64url } from '../primitives/base64url.ts';
import { assertLength } from '../primitives/bytes.ts';
import { idToString, parseId } from '../primitives/identifiers.ts';

const FRAGMENT_LABEL = String(FRAGMENT_VERSION);
const LINK_PATH_PREFIX = '/m/';
const VERSION_LABEL = /^[1-9]\d{0,8}$/;
const TEXT_BEFORE_FRAGMENT = /^https?:\/\/[\d.:[\]a-z-]+(\/[^?#]*)(?:\?[^#]*)?$/i;

export interface Fragment {
  readonly kdfId: KdfId;
  readonly secret: Uint8Array<ArrayBuffer>;
}

export interface Link extends Fragment {
  readonly id: Uint8Array<ArrayBuffer>;
}

export function fragment(kdfId: KdfId, secret: Uint8Array): string {
  assertLength(secret, SECRET_LENGTH, 'secret');
  return `${FRAGMENT_LABEL}.${String(kdfId)}.${encodeBase64url(secret)}`;
}

export function formatLink(base: string, id: Uint8Array, kdfId: KdfId, secret: Uint8Array): string {
  const origin = base.endsWith('/') ? base.slice(0, -1) : base;
  return `${origin}${LINK_PATH_PREFIX}${idToString(id)}#${fragment(kdfId, secret)}`;
}

export function parseFragment(text: string): Fragment {
  const body = text.startsWith('#') ? text.slice(1) : text;
  assertKnownVersion(body);
  return parseCurrentFragment(body);
}

export function parsePath(path: string): Uint8Array<ArrayBuffer> {
  if (!path.startsWith(LINK_PATH_PREFIX)) {
    throw new LinkError();
  }
  return orLinkError(() => parseId(path.slice(LINK_PATH_PREFIX.length)));
}

export function parseLink(link: string): Link {
  const fragmentStart = link.indexOf('#');
  const path = fragmentStart === -1 ? undefined : TEXT_BEFORE_FRAGMENT.exec(link.slice(0, fragmentStart))?.[1];
  if (path === undefined) {
    throw new LinkError();
  }
  const body = link.slice(fragmentStart + 1);
  assertKnownVersion(body);
  return { id: parsePath(path), ...parseCurrentFragment(body) };
}

export function assertProfile(fragmentKdfId: KdfId, serverKdfId: number): void {
  if (fragmentKdfId !== serverKdfId) {
    throw new KdfMismatchError();
  }
}

function assertKnownVersion(body: string): void {
  const [label = ''] = body.split('.', 1);
  if (VERSION_LABEL.test(label) && Number(label) > FRAGMENT_VERSION) {
    throw new LinkVersionError();
  }
}

function parseCurrentFragment(body: string): Fragment {
  const parts = body.split('.');
  const [label, kdfText = '', secretText = ''] = parts;
  if (label !== FRAGMENT_LABEL || parts.length !== 3) {
    throw new LinkError();
  }
  if (kdfText === String(KDF_RETIRED)) {
    decodeSecret(secretText).fill(0);
    throw new LinkRetiredKdfError();
  }
  const kdfId = orLinkError(() => parseKdfId(kdfText));
  return { kdfId, secret: decodeSecret(secretText) };
}

function decodeSecret(text: string): Uint8Array<ArrayBuffer> {
  return orLinkError(() => decodeBase64urlExact(text, SECRET_LENGTH));
}

function orLinkError<T>(parse: () => T): T {
  try {
    return parse();
  } catch {
    throw new LinkError();
  }
}
