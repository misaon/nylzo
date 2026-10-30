import {
  deriveLink,
  encodeBase64url,
  formatLink,
  type Header,
  idToString,
  KDF_NONE,
  KDF_SCRYPT,
  type Meta,
  newHeader,
  newRequestKey,
  newSecret,
  type Policy,
  requestKey,
  seal,
} from '@nylzo/core';
import { deriveAll } from '@nylzo/core/scrypt';

import { createApi, type Status } from '../../../app/src/shared/api/api.ts';
import { aad, headerBytes } from '../../../core/src/envelope/header.ts';
import { arrayBufferBacked, concatenate } from '../../../core/src/primitives/bytes.ts';
import { BASE_URL } from './servers.ts';

export interface MessageFile {
  readonly name: string;
  readonly mimeType: string;
  readonly buffer: Buffer;
}

export interface MessageOptions {
  readonly base?: string;
  readonly text?: string;
  readonly file?: MessageFile;
  readonly passphrase?: string;
  readonly opensMax?: number;
  readonly readKeyStartsWithZero?: boolean;
  readonly plaintext?: Uint8Array<ArrayBuffer>;
}

export interface CreatedMessage {
  readonly link: string;
  readonly id: string;
  readonly secret: string;
  readonly status: () => Promise<Status>;
}

export async function createMessageThroughApi(options: MessageOptions = {}): Promise<CreatedMessage> {
  const base = options.base ?? BASE_URL;
  const passphrase = options.passphrase ?? '';
  const kdfId = passphrase === '' ? KDF_NONE : KDF_SCRYPT;
  const api = createApi({ base });
  const { create } = await api.instance();
  let secret = newSecret();
  while (options.readKeyStartsWithZero === true && !(await readKeyStartsWithZero(create.id, secret))) {
    secret = newSecret();
  }
  const { link, content } = await deriveAll(create.id, secret, kdfId, passphrase);
  const policy = { opensMax: options.opensMax ?? 1, ttlSeconds: 3600 };
  const header = newHeader(kdfId);
  const object =
    options.plaintext === undefined
      ? await seal(content.contentKey, header, create.id, policy, { meta: metaOf(options), file: options.file?.buffer })
      : await sealPlaintext(content.contentKey, header, create.id, policy, options.plaintext);
  const readKey = await requestKey(link.readSeed);
  const verifier = kdfId === KDF_NONE ? undefined : await requestKey(content.verifierSeed);
  const manageKey = await newRequestKey();
  await api.create({
    id: create.id,
    idToken: create.idToken,
    readKey: readKey.publicKey,
    ...(verifier && { verifierKey: verifier.publicKey }),
    manageKey: manageKey.publicKey,
    kdfId,
    ...policy,
    object,
  });
  return {
    link: formatLink(base, create.id, kdfId, secret),
    id: idToString(create.id),
    secret: encodeBase64url(secret),
    status: () => api.status(create.id, manageKey),
  };
}

async function sealPlaintext(
  key: CryptoKey,
  header: Header,
  id: Uint8Array,
  policy: Policy,
  plaintext: Uint8Array<ArrayBuffer>,
): Promise<Uint8Array<ArrayBuffer>> {
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: arrayBufferBacked(header.iv), additionalData: aad(header, id, policy) },
    key,
    plaintext,
  );
  return concatenate(headerBytes(header), new Uint8Array(ciphertext));
}

async function readKeyStartsWithZero(id: Uint8Array, secret: Uint8Array): Promise<boolean> {
  const { readSeed } = await deriveLink(id, secret);
  return readSeed[0] === 0;
}

function metaOf({ text, file }: MessageOptions): Meta {
  if (file === undefined) {
    return { text: text ?? 'from the api' };
  }
  return {
    ...(text !== undefined && { text }),
    file: { name: file.name, mime: file.mimeType, size: file.buffer.length },
  };
}
