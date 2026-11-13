import {
  createRegistrationRequest,
  deriveContent,
  deriveLink,
  derivePassphraseSalt,
  encodeBase64url,
  finalizeRegistrationRequest,
  formatLink,
  identityStretch,
  idToString,
  KDF_NONE,
  KDF_SCRYPT,
  newHeader,
  newRequestKey,
  newSecret,
  newShareKey,
  objectHash,
  opaquePassword,
  requestKey,
  seal,
  unwrapShare,
} from '@nylzo/core';
import type { ContentKeys, Header, Meta, Policy } from '@nylzo/core';
import { stretchScrypt } from '@nylzo/core/scrypt';

import { createApi } from '../../../app/src/shared/api/api.ts';
import type { Status } from '../../../app/src/shared/api/api.ts';
import { commitment } from '../../../core/src/envelope/envelope.ts';
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
  const ttlSeconds = 3600;
  const api = createApi({ base });
  const shareKey = newShareKey();
  const secret = newSecret();
  const password = opaquePassword(await derivePassphraseSalt(secret), kdfId, passphrase);
  const { request, state } = createRegistrationRequest(password);
  const { create } = await api.instance({ ttlSeconds, shareKey: shareKey.publicKey, registrationRequest: request });
  if (create === undefined) {
    throw new Error('the instance issued no id');
  }
  const policy = { opensMax: options.opensMax ?? 1, ttlSeconds, deadline: create.deadline };
  const stretch =
    kdfId === KDF_NONE ? identityStretch : (message: Uint8Array) => Promise.resolve(stretchScrypt(message));
  const registration = await finalizeRegistrationRequest(state, create.registration, stretch);
  const share = await unwrapShare(shareKey, create.share);
  const content = await deriveContent(create.id, secret, registration.exportKey, share);
  const link = await deriveLink(create.id, secret);
  const header = newHeader(kdfId);
  const object =
    options.plaintext === undefined
      ? await seal(content, header, create.id, policy, { meta: metaOf(options), file: options.file?.buffer })
      : await sealPlaintext(content, header, create.id, policy, options.plaintext);
  const readKey = await requestKey(link.readSeed);
  const manageKey = await newRequestKey();
  await api.create({
    id: create.id,
    idToken: create.idToken,
    epoch: create.epoch,
    readKey: readKey.publicKey,
    registration: registration.record,
    manageKey: manageKey.publicKey,
    kdfId,
    ...policy,
    object,
  });
  return {
    link: formatLink(base, create.id, kdfId, secret, await objectHash(object)),
    id: idToString(create.id),
    secret: encodeBase64url(secret),
    status: () => api.status(create.id, manageKey),
  };
}

async function sealPlaintext(
  content: ContentKeys,
  header: Header,
  id: Uint8Array,
  policy: Policy,
  plaintext: Uint8Array<ArrayBuffer>,
): Promise<Uint8Array<ArrayBuffer>> {
  const committed = await commitment(content.commitmentKey, header, id, policy);
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: arrayBufferBacked(header.iv), additionalData: aad(header, committed, id, policy) },
    content.contentKey,
    plaintext,
  );
  return concatenate(headerBytes(header), committed, new Uint8Array(ciphertext));
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
