import {
  deriveLink,
  encodeBase64url,
  formatLink,
  idToString,
  KDF_NONE,
  KDF_SCRYPT,
  type Meta,
  newHeader,
  newRequestKey,
  newSecret,
  requestKey,
  seal,
} from '@nylzo/core';
import { deriveAll } from '@nylzo/core/scrypt';

import { createApi, type Status } from '../../../app/src/shared/api/api.ts';
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
  const object = await seal(content.k, newHeader(kdfId), create.id, policy, {
    meta: metaOf(options),
    file: options.file?.buffer,
  });
  const readKey = await requestKey(link.r);
  const verifier = kdfId === KDF_NONE ? undefined : await requestKey(content.v);
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

async function readKeyStartsWithZero(id: Uint8Array, secret: Uint8Array): Promise<boolean> {
  const { r } = await deriveLink(id, secret);
  return r[0] === 0;
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
