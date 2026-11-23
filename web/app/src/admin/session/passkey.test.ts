import { encodeBase64url } from '@nylzo/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ResponseError } from '../../shared/api/api-errors.ts';
import { createPasskey, passkeysSupported, usePasskey } from './passkey.ts';
import { KeyTypeError } from './problems.ts';

const ED25519 = -8;

function buffer(...values: number[]): ArrayBuffer {
  return new Uint8Array(values).buffer;
}

function encoded(...values: number[]): string {
  return encodeBase64url(new Uint8Array(values));
}

class FakeAttestationResponse {
  readonly clientDataJSON = buffer(1, 2);
  readonly publicKey: ArrayBuffer | null;

  constructor(publicKey: ArrayBuffer | null) {
    this.publicKey = publicKey;
  }

  getPublicKey(): ArrayBuffer | null {
    return this.publicKey;
  }

  getAuthenticatorData(): ArrayBuffer {
    return buffer(3, 4);
  }

  getPublicKeyAlgorithm(): number {
    return ED25519;
  }
}

class FakeAssertionResponse {
  readonly clientDataJSON = buffer(5);
  readonly authenticatorData = buffer(6);
  readonly signature = buffer(7, 8, 9);
}

class LegacyPublicKeyCredential {
  readonly type = 'public-key';
  static readonly isUserVerifyingPlatformAuthenticatorAvailable = vi.fn<() => Promise<boolean>>();
}

class FakePublicKeyCredential {
  static readonly parseCreationOptionsFromJSON = vi.fn<(json: unknown) => unknown>((json) => ({ creation: json }));
  static readonly parseRequestOptionsFromJSON = vi.fn<(json: unknown) => unknown>((json) => ({ request: json }));
  readonly rawId = buffer(250, 251);
  readonly response: unknown;

  constructor(response: unknown) {
    this.response = response;
  }
}

const credentials = {
  create: vi.fn<(options: unknown) => Promise<unknown>>(),
  get: vi.fn<(options: unknown) => Promise<unknown>>(),
};

const ceremony = { public_key: { challenge: 'AAAA', rp: { id: 'nylzo.example' } } };

function stubBrowser(changes: Record<string, unknown> = {}): void {
  vi.stubGlobal('PublicKeyCredential', FakePublicKeyCredential);
  vi.stubGlobal('AuthenticatorAttestationResponse', FakeAttestationResponse);
  vi.stubGlobal('AuthenticatorAssertionResponse', FakeAssertionResponse);
  vi.stubGlobal('navigator', { credentials });
  for (const [name, value] of Object.entries(changes)) {
    vi.stubGlobal(name, value);
  }
}

describe('passkeys', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    stubBrowser();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  describe('passkeysSupported', () => {
    it('accepts a browser with the JSON ceremony helpers, passkey creation and Ed25519 signing', async () => {
      await expect(passkeysSupported()).resolves.toBe(true);
    });

    it.each([
      ['no WebAuthn', { PublicKeyCredential: undefined }],
      ['no creation options parser', { PublicKeyCredential: LegacyPublicKeyCredential }],
      [
        'no request options parser',
        { PublicKeyCredential: { parseCreationOptionsFromJSON: FakePublicKeyCredential.parseCreationOptionsFromJSON } },
      ],
      ['no credential container', { navigator: {} }],
      ['no way to create a credential', { navigator: { credentials: { get: credentials.get } } }],
    ])('refuses a browser with %s', async (_, changes) => {
      stubBrowser(changes);
      await expect(passkeysSupported()).resolves.toBe(false);
    });

    it('refuses a browser that cannot sign with Ed25519', async () => {
      vi.spyOn(crypto.subtle, 'generateKey').mockRejectedValue(new DOMException('no', 'NotSupportedError'));
      await expect(passkeysSupported()).resolves.toBe(false);
    });
  });

  describe('createPasskey', () => {
    it('creates a passkey from the server options and hands over its public key', async () => {
      const response = new FakeAttestationResponse(buffer(10, 11));
      credentials.create.mockResolvedValue(new FakePublicKeyCredential(response));

      await expect(createPasskey(ceremony)).resolves.toEqual({
        credential_id: encoded(250, 251),
        client_data: encoded(1, 2),
        authenticator_data: encoded(3, 4),
        public_key: encoded(10, 11),
        algorithm: ED25519,
      });
      expect(FakePublicKeyCredential.parseCreationOptionsFromJSON).toHaveBeenCalledWith(ceremony.public_key);
      expect(credentials.create).toHaveBeenCalledWith({ publicKey: { creation: ceremony.public_key } });
    });

    it('refuses a ceremony without public key options before asking the browser', async () => {
      await expect(createPasskey({ public_key: 'AAAA' })).rejects.toBeInstanceOf(ResponseError);
      expect(credentials.create).not.toHaveBeenCalled();
    });

    it('treats a browser that returns no passkey as a cancelled ceremony', async () => {
      credentials.create.mockResolvedValue(null);
      await expect(createPasskey(ceremony)).rejects.toMatchObject({ name: 'NotAllowedError' });
    });

    it('refuses a passkey that answers without an attestation', async () => {
      credentials.create.mockResolvedValue(new FakePublicKeyCredential(new FakeAssertionResponse()));
      await expect(createPasskey(ceremony)).rejects.toBeInstanceOf(ResponseError);
    });

    it('refuses a passkey whose public key the browser cannot hand over', async () => {
      credentials.create.mockResolvedValue(new FakePublicKeyCredential(new FakeAttestationResponse(null)));
      await expect(createPasskey(ceremony)).rejects.toBeInstanceOf(KeyTypeError);
    });
  });

  describe('usePasskey', () => {
    it('signs the server challenge with a passkey and hands over the assertion', async () => {
      credentials.get.mockResolvedValue(new FakePublicKeyCredential(new FakeAssertionResponse()));

      await expect(usePasskey(ceremony)).resolves.toEqual({
        credential_id: encoded(250, 251),
        client_data: encoded(5),
        authenticator_data: encoded(6),
        signature: encoded(7, 8, 9),
      });
      expect(credentials.get).toHaveBeenCalledWith({ publicKey: { request: ceremony.public_key } });
    });

    it('refuses a ceremony without public key options before asking the browser', async () => {
      await expect(usePasskey({})).rejects.toBeInstanceOf(ResponseError);
      expect(credentials.get).not.toHaveBeenCalled();
    });

    it('treats an answer that is not a passkey as a cancelled ceremony', async () => {
      credentials.get.mockResolvedValue({ type: 'password' });
      await expect(usePasskey(ceremony)).rejects.toMatchObject({ name: 'NotAllowedError' });
    });

    it('refuses a passkey that answers without an assertion', async () => {
      const response = new FakeAttestationResponse(buffer(1));
      credentials.get.mockResolvedValue(new FakePublicKeyCredential(response));
      await expect(usePasskey(ceremony)).rejects.toBeInstanceOf(ResponseError);
    });
  });
});
