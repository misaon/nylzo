import {
  createRegistrationRequest,
  finalizeRegistrationRequest,
  generateKE1,
  generateKE3,
  identityStretch,
  idToString,
  KDF_NONE,
  newRequestKey,
  newShareKey,
  opaqueContext,
} from '@nylzo/core';
import type { RequestKey } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import type { Unlock } from './api.ts';
import { fakeApi } from './fake-api.ts';
import type { FakeApi, FakeOptions } from './fake-api.ts';

interface SentMessage {
  readonly id: Uint8Array;
  readonly readKey: RequestKey;
  readonly manageKey: RequestKey;
  readonly unlock: Unlock;
  readonly unlockLease: (lease: string) => Unlock;
}

const password = new Uint8Array(32).fill(1);

async function sentMessage(api: FakeApi, opensMax = 1): Promise<SentMessage> {
  const { request: registrationRequest, state } = createRegistrationRequest(password);
  const { create } = await api.instance({
    ttlSeconds: 3600,
    shareKey: newShareKey().publicKey,
    registrationRequest,
  });
  const registration = await finalizeRegistrationRequest(state, create.registration, identityStretch);
  const readKey = await newRequestKey();
  const manageKey = await newRequestKey();
  await api.create({
    id: create.id,
    idToken: create.idToken,
    epoch: create.epoch,
    deadline: create.deadline,
    readKey: readKey.publicKey,
    registration: registration.record,
    manageKey: manageKey.publicKey,
    kdfId: KDF_NONE,
    opensMax,
    ttlSeconds: 3600,
    object: new Uint8Array(16).fill(9),
  });
  const unlockLease =
    (lease?: string): Unlock =>
    async () => {
      const { ke1, login } = generateKE1(password);
      const handshake = await api.handshake(create.id, readKey, ke1, lease);
      const config = { context: opaqueContext(create.id, KDF_NONE), stretch: identityStretch };
      const { ke3 } = await generateKE3(login, handshake.ke2, config);
      return { challenge: handshake.challenge, ke3 };
    };
  return { id: create.id, readKey, manageKey, unlock: unlockLease(), unlockLease };
}

async function fakeWithMessage(options: FakeOptions = {}, opensMax = 1) {
  const api = fakeApi(options);
  return { api, message: await sentMessage(api, opensMax) };
}

const shareKeys = () => newShareKey();

const unknownChallenge: Unlock = () => Promise.resolve({ challenge: new Uint8Array(36), ke3: new Uint8Array(64) });

describe('the fake server keeps the rules the flow tests rely on', () => {
  it('opens a message once for the KE3 of its own handshake', async () => {
    const { api, message } = await fakeWithMessage();
    const claimed = await api.claim(message.id, message.readKey, { unlock: message.unlock, shareKeys });
    expect(claimed.object).toEqual(new Uint8Array(16).fill(9));
    await expect(api.claim(message.id, message.readKey, { unlock: message.unlock, shareKeys })).rejects.toMatchObject({
      kind: 'gone',
      state: 'opened',
    });
  });

  it('refuses a claim when the message ended after its handshake', async () => {
    const { api, message } = await fakeWithMessage();
    const login = await message.unlock();
    await api.cancel(message.id, message.manageKey);
    const unlock: Unlock = () => Promise.resolve(login);
    await expect(api.claim(message.id, message.readKey, { unlock, shareKeys })).rejects.toMatchObject({
      kind: 'gone',
      state: 'cancelled',
    });
  });

  it('refuses a claim whose challenge no handshake of the message issued', async () => {
    const { api, message } = await fakeWithMessage();
    await expect(api.claim(message.id, message.readKey, { unlock: unknownChallenge, shareKeys })).rejects.toMatchObject(
      {
        kind: 'not_found',
      },
    );
  });

  it('counts a wrong KE3 as a failed passphrase and locks the handshake after the limit', async () => {
    const { api, message } = await fakeWithMessage({ lockAfter: 1 });
    const wrong: Unlock = async () => ({ ...(await message.unlock()), ke3: new Uint8Array(64) });
    await expect(api.claim(message.id, message.readKey, { unlock: wrong, shareKeys })).rejects.toMatchObject({
      kind: 'passphrase',
      attemptsLeft: 0,
      retryAfter: 3600,
    });
    await expect(message.unlock()).rejects.toMatchObject({ kind: 'passphrase', attemptsLeft: 0 });
  });

  it('serves the object of a held lease only, with or without a new handshake', async () => {
    const { api, message } = await fakeWithMessage();
    const { lease } = await api.claim(message.id, message.readKey, { unlock: message.unlock, shareKeys });
    const resumed = await api.blob(message.id, message.readKey, lease, shareKeys);
    expect(resumed.object).toEqual(new Uint8Array(16).fill(9));
    await expect(api.blob(message.id, message.readKey, 'lease-other', shareKeys)).rejects.toMatchObject({
      kind: 'gone',
    });
  });

  it('burns an opened message while its lease is held and refuses to end it twice', async () => {
    const { api, message } = await fakeWithMessage();
    const { lease } = await api.claim(message.id, message.readKey, { unlock: message.unlock, shareKeys });
    await api.burn(message.id, message.readKey, message.unlockLease(lease));
    await expect(api.burn(message.id, message.readKey, message.unlockLease(lease))).rejects.toMatchObject({
      kind: 'gone',
      state: 'deleted',
    });
    expect(api.calls).toEqual(['instance+id', 'create', 'handshake', 'claim', 'handshake+lease', 'burn', 'burn']);
  });

  it('answers the status only to the manage key of the sender', async () => {
    const { api, message } = await fakeWithMessage();
    await expect(api.status(message.id, message.readKey)).rejects.toMatchObject({ kind: 'not_found' });
    await expect(api.status(message.id, message.manageKey)).resolves.toMatchObject({ state: 'available' });
  });

  it('runs a handshake for a message stored without an issued id', async () => {
    const { api, message } = await fakeWithMessage();
    const issued = api.stored.get(idToString(message.id));
    if (issued === undefined) {
      throw new Error('the fake stored no message');
    }
    const id = crypto.getRandomValues(new Uint8Array(16));
    await api.create({ ...issued.request, id });
    const { ke1 } = generateKE1(password);
    await expect(api.handshake(id, message.readKey, ke1)).resolves.toMatchObject({ attemptsLeft: 4 });
  });

  it('serves no transparency report', async () => {
    await expect(fakeApi().transparency()).rejects.toThrow(Error);
  });
});
