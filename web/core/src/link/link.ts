import { KdfMismatchError, LinkError, LinkReleasedError, LinkRetiredKdfError, LinkVersionError } from '../errors.ts';
import { KDF_RETIRED, type KdfId, parseKdfId } from '../keys/kdf.ts';
import { FRAGMENT_VERSION, OBJECT_HASH_LENGTH, SECRET_LENGTH } from '../limits.ts';
import { decodeBase64urlExact, encodeBase64url } from '../primitives/base64url.ts';
import { assertLength } from '../primitives/bytes.ts';
import { idToString, parseId } from '../primitives/identifiers.ts';

const LINK_PATH = '/r';
const RELEASED_PATH_PREFIX = '/m/';
const VERSION_LABEL = /^[1-9]\d{0,8}$/;
const TEXT_BEFORE_FRAGMENT = /^https?:\/\/[\d.:[\]a-z-]+(\/[^?#]*)(?:\?[^#]*)?$/i;
const LABEL = String(FRAGMENT_VERSION);
const RELEASED_PROFILES: ReadonlySet<string> = new Set(['0', '1', '2']);

export interface Link {
  readonly kdfId: KdfId;
  readonly id: Uint8Array<ArrayBuffer>;
  readonly secret: Uint8Array<ArrayBuffer>;
  readonly objectHash: Uint8Array<ArrayBuffer>;
}

export function formatLink(
  base: string,
  id: Uint8Array,
  kdfId: KdfId,
  secret: Uint8Array,
  objectHash: Uint8Array,
): string {
  assertLength(secret, SECRET_LENGTH, 'secret');
  assertLength(objectHash, OBJECT_HASH_LENGTH, 'object hash');
  const origin = base.endsWith('/') ? base.slice(0, -1) : base;
  const fields = [LABEL, String(kdfId), idToString(id), encodeBase64url(secret), encodeBase64url(objectHash)];
  return `${origin}${LINK_PATH}#${fields.join('.')}`;
}

// The read page of a link of protocol version 1 still loads, only to say that the upgrade erased its message.
export function isReadPath(path: string): boolean {
  return path === LINK_PATH || path.startsWith(RELEASED_PATH_PREFIX);
}

// Reads the link from the page path and the fragment, as the address bar held them.
export function parseFragment(path: string, text: string): Link {
  return linkOf(path, text.startsWith('#') ? text.slice(1) : text);
}

export function parseLink(link: string): Link {
  const fragmentStart = link.indexOf('#');
  const path = fragmentStart === -1 ? undefined : TEXT_BEFORE_FRAGMENT.exec(link.slice(0, fragmentStart))?.[1];
  if (path === undefined) {
    throw new LinkError();
  }
  return linkOf(path, link.slice(fragmentStart + 1));
}

function linkOf(path: string, body: string): Link {
  assertKnownVersion(body);
  if (body.split('.', 1)[0] === LABEL) {
    if (path !== LINK_PATH) {
      throw new LinkError();
    }
    return parseCurrentFragment(body);
  }
  throw releasedLink(path, body) ? new LinkReleasedError() : new LinkError();
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

function releasedLink(path: string, body: string): boolean {
  const parts = body.split('.');
  const [label, kdfText = '', secretText = ''] = parts;
  if (
    label !== '1' ||
    parts.length !== 3 ||
    !path.startsWith(RELEASED_PATH_PREFIX) ||
    !RELEASED_PROFILES.has(kdfText)
  ) {
    return false;
  }
  try {
    parseId(path.slice(RELEASED_PATH_PREFIX.length));
    decodeBase64urlExact(secretText, SECRET_LENGTH).fill(0);
    return true;
  } catch {
    return false;
  }
}

function parseCurrentFragment(body: string): Link {
  const parts = body.split('.');
  if (parts.length !== 5) {
    throw new LinkError();
  }
  const [, kdfText = '', idText = '', secretText = '', hashText = ''] = parts;
  const id = orLinkError(() => parseId(idText));
  const secret = decodeField(secretText, SECRET_LENGTH);
  let objectHash: Uint8Array<ArrayBuffer>;
  try {
    objectHash = decodeField(hashText, OBJECT_HASH_LENGTH);
  } catch (error) {
    secret.fill(0);
    throw error;
  }
  return { kdfId: profile(kdfText, secret), id, secret, objectHash };
}

function profile(text: string, secret: Uint8Array): KdfId {
  if (text === String(KDF_RETIRED)) {
    secret.fill(0);
    throw new LinkRetiredKdfError();
  }
  try {
    return parseKdfId(text);
  } catch {
    secret.fill(0);
    throw new LinkError();
  }
}

function decodeField(text: string, length: number): Uint8Array<ArrayBuffer> {
  return orLinkError(() => decodeBase64urlExact(text, length));
}

function orLinkError<T>(parse: () => T): T {
  try {
    return parse();
  } catch {
    throw new LinkError();
  }
}
