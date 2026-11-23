import { describe, expect, it, vi } from 'vitest';

import rfcFixture from '../../../../spec/opaque-rfc-vectors.json' with { type: 'json' };
import { hexToBytes, kdfIdOf, vectors } from '../../test/vectors.ts';
import type { OpaqueVector } from '../../test/vectors.ts';
import { EnvelopeRecoveryError, OpaqueMessageError, ServerAuthenticationError } from '../errors.ts';
import { decodeBase64url } from '../primitives/base64url.ts';
import { concatenate } from '../primitives/bytes.ts';
import { parseId } from '../primitives/identifiers.ts';
import { stretchScrypt } from '../scrypt.ts';
import { createRegistrationResponse, generateKE2, newServerKeys, serverKeysFrom } from '../testing/opaque-server.ts';
import { derivePassphraseSalt } from './derive.ts';
import { KDF_SCRYPT, opaqueContext, opaquePassword } from './kdf.ts';
import {
  createRegistrationRequest,
  deserializeElement,
  finalizeRegistrationRequest,
  generateKE1,
  generateKE3,
  identityStretch,
  KE2_LENGTH,
  wipeLogin,
} from './opaque.ts';
import type { OpaqueConfig, Stretch } from './opaque.ts';

const rfc = rfcFixture;
const real = firstOf(rfc.opaque_rfc9807);

function firstOf<Vector>(list: readonly Vector[]): Vector {
  const [first] = list;
  if (first === undefined) {
    throw new Error('spec/opaque-rfc-vectors.json lacks RFC 9807 real vector 1');
  }
  return first;
}
const rfcConfig: OpaqueConfig = { context: hexToBytes(real.context), stretch: identityStretch };

function rfcLogin() {
  const password = hexToBytes(real.password);
  return generateKE1(
    password,
    hexToBytes(real.blind_login),
    hexToBytes(real.client_nonce),
    hexToBytes(real.client_keyshare_seed),
  );
}

describe('RFC 9497 OPRF(ristretto255, SHA-512) vectors through the OPAQUE client', () => {
  it.each(rfc.oprf_rfc9497.vectors)('$name', async (vector) => {
    const input = hexToBytes(vector.input);
    const { request, state } = createRegistrationRequest(input, hexToBytes(vector.blind));
    expect(request.toHex()).toBe(vector.blinded_element);
    // The registration record starts with the client public key, which follows from the OPRF output alone.
    const serverPublicKey = hexToBytes(real.server_public_key);
    const response = new Uint8Array([...hexToBytes(vector.evaluation_element), ...serverPublicKey]);
    let output: Uint8Array | undefined;
    const capture: Stretch = (message) => {
      output = new Uint8Array(message);
      return identityStretch(message);
    };
    await finalizeRegistrationRequest(state, response, capture, new Uint8Array(32));
    expect(output?.toHex()).toBe(vector.output);
  });
});

describe('RFC 9807 OPAQUE-3DH real vector 1, client steps', () => {
  it('registers', async () => {
    const { request, state } = createRegistrationRequest(
      hexToBytes(real.password),
      hexToBytes(real.blind_registration),
    );
    expect(request.toHex()).toBe(real.registration_request);
    const registration = await finalizeRegistrationRequest(
      state,
      hexToBytes(real.registration_response),
      identityStretch,
      hexToBytes(real.envelope_nonce),
    );
    expect(registration.record.toHex()).toBe(real.registration_upload);
    expect(registration.exportKey.toHex()).toBe(real.export_key);
  });

  it('logs in', async () => {
    const { ke1, login } = rfcLogin();
    expect(ke1.toHex()).toBe(real.ke1);
    const result = await generateKE3(login, hexToBytes(real.ke2), rfcConfig);
    expect(result.ke3.toHex()).toBe(real.ke3);
    expect(result.sessionKey.toHex()).toBe(real.session_key);
    expect(result.exportKey.toHex()).toBe(real.export_key);
  });

  it('matches the server steps of the fake server used by the client tests', () => {
    const keys = serverKeysFrom(hexToBytes(real.server_private_key), hexToBytes(real.oprf_seed));
    expect(keys.publicKey.toHex()).toBe(real.server_public_key);
    const credential = hexToBytes(real.credential_identifier);
    const response = createRegistrationResponse(hexToBytes(real.registration_request), keys, credential);
    expect(response.toHex()).toBe(real.registration_response);
    const { ke2, expectedClientMac } = generateKE2({
      keys,
      record: hexToBytes(real.registration_upload),
      credentialIdentifier: credential,
      ke1: hexToBytes(real.ke1),
      context: rfcConfig.context,
      fixed: {
        maskingNonce: hexToBytes(real.masking_nonce),
        serverNonce: hexToBytes(real.server_nonce),
        keyshareSeed: hexToBytes(real.server_keyshare_seed),
      },
    });
    expect(ke2.toHex()).toBe(real.ke2);
    expect(expectedClientMac.toHex()).toBe(real.ke3);
  });

  it('tells a wrong password from an answer of another server', async () => {
    const wrong = generateKE1(new Uint8Array([...hexToBytes(real.password), 0x78]));
    await expect(generateKE3(wrong.login, hexToBytes(real.ke2), rfcConfig)).rejects.toBeInstanceOf(
      EnvelopeRecoveryError,
    );
    const changedMac = hexToBytes(real.ke2);
    changedMac[KE2_LENGTH - 1] = (changedMac[KE2_LENGTH - 1] ?? 0) ^ 1;
    await expect(generateKE3(rfcLogin().login, changedMac, rfcConfig)).rejects.toBeInstanceOf(
      ServerAuthenticationError,
    );
    const otherContext = { ...rfcConfig, context: new Uint8Array([1]) };
    await expect(generateKE3(rfcLogin().login, hexToBytes(real.ke2), otherContext)).rejects.toBeInstanceOf(
      ServerAuthenticationError,
    );
  });

  it('refuses an OPRF input that Finalize cannot prefix with its length, and completes the longest one', async () => {
    const longest = new Uint8Array(0xff_ff);
    const tooLong = new Uint8Array(longest.length + 1);
    expect(() => createRegistrationRequest(tooLong)).toThrow(OpaqueMessageError);
    expect(() => generateKE1(tooLong)).toThrow(OpaqueMessageError);
    const keys = serverKeysFrom(hexToBytes(real.server_private_key), hexToBytes(real.oprf_seed));
    const { request, state } = createRegistrationRequest(longest);
    const response = createRegistrationResponse(request, keys, hexToBytes(real.credential_identifier));
    await expect(finalizeRegistrationRequest(state, response, identityStretch)).resolves.toBeDefined();
  });

  it('refuses a KE2 of the wrong length or with an identity element', async () => {
    const ke2 = hexToBytes(real.ke2);
    await expect(generateKE3(rfcLogin().login, ke2.subarray(1), rfcConfig)).rejects.toBeInstanceOf(OpaqueMessageError);
    const identityKeyshare = new Uint8Array(ke2);
    identityKeyshare.fill(0, 192 + 32, 192 + 64);
    await expect(generateKE3(rfcLogin().login, identityKeyshare, rfcConfig)).rejects.toBeInstanceOf(OpaqueMessageError);
    expect(() => createRegistrationRequest(hexToBytes(real.password), new Uint8Array(32))).toThrow(OpaqueMessageError);
  });
});

function stretchFor(vector: OpaqueVector, onProgress?: (fraction: number) => void): Stretch {
  return kdfIdOf(vector.kdf_id) === KDF_SCRYPT
    ? (message) => Promise.resolve(stretchScrypt(message, onProgress))
    : identityStretch;
}

describe('the OPAQUE client refuses malformed server messages and inputs (RFC 9497, RFC 9807)', () => {
  const password = hexToBytes(real.password);
  const element = hexToBytes(real.server_public_key);
  const nonCanonical = new Uint8Array(32).fill(0xff);

  it.each([
    ['a short element', element.subarray(1)],
    ['a non-canonical encoding', nonCanonical],
    ['the identity', new Uint8Array(32)],
  ])('refuses %s as an element', (_, bytes) => {
    expect(() => deserializeElement(bytes)).toThrow(OpaqueMessageError);
  });

  it('refuses a blind that is not a canonical scalar', () => {
    expect(() => createRegistrationRequest(password, nonCanonical)).toThrow(OpaqueMessageError);
  });

  it('refuses a registration response of the wrong length, a bad server key or a short envelope nonce', async () => {
    const { state } = createRegistrationRequest(password, hexToBytes(real.blind_registration));
    const response = hexToBytes(real.registration_response);
    const badServerKey = concatenate(response.subarray(0, 32), nonCanonical);
    await expect(finalizeRegistrationRequest(state, response.subarray(1), identityStretch)).rejects.toBeInstanceOf(
      OpaqueMessageError,
    );
    await expect(finalizeRegistrationRequest(state, badServerKey, identityStretch)).rejects.toBeInstanceOf(
      OpaqueMessageError,
    );
    await expect(
      finalizeRegistrationRequest(state, response, identityStretch, new Uint8Array(31)),
    ).rejects.toBeInstanceOf(OpaqueMessageError);
  });

  it('refuses a client nonce or a keyshare seed of the wrong length', () => {
    expect(() => generateKE1(password, undefined, new Uint8Array(31))).toThrow(OpaqueMessageError);
    expect(() => generateKE1(password, undefined, new Uint8Array(32), new Uint8Array(31))).toThrow(OpaqueMessageError);
  });

  it('draws the blind again when the random scalar is zero', () => {
    const random = vi.spyOn(crypto, 'getRandomValues');
    random.mockImplementationOnce((array) => array);
    try {
      const fresh = createRegistrationRequest(password);
      expect(random).toHaveBeenCalledTimes(2);
      expect(fresh.state.blind).not.toBe(0n);
    } finally {
      random.mockRestore();
    }
  });
});

describe('a login with fresh randomness', () => {
  it('recovers the export key of the registration and wipes the password copy', async () => {
    const keys = newServerKeys();
    const credential = new Uint8Array(16).fill(3);
    const context = new Uint8Array([1, 2]);
    const password = new Uint8Array([7, 7, 7]);
    const { request, state } = createRegistrationRequest(password);
    const registration = await finalizeRegistrationRequest(
      state,
      createRegistrationResponse(request, keys, credential),
      identityStretch,
    );
    const { ke1, login } = generateKE1(password);
    const { ke2, expectedClientMac } = generateKE2({
      keys,
      record: registration.record,
      credentialIdentifier: credential,
      ke1,
      context,
    });
    const result = await generateKE3(login, ke2, { context, stretch: identityStretch });
    expect(result.exportKey).toEqual(registration.exportKey);
    expect(result.ke3).toEqual(expectedClientMac);
    wipeLogin(login);
    wipeLogin(undefined);
    expect(login.password.every((byte) => byte === 0)).toBe(true);
  });
});

describe('OPAQUE runs with the configuration of spec 2.1', () => {
  it.each(vectors.opaque)('$name', { timeout: 120_000 }, async (vector) => {
    const id = parseId(vector.id);
    const kdfId = kdfIdOf(vector.kdf_id);
    const salt = await derivePassphraseSalt(hexToBytes(vector.s));
    expect(salt.toHex()).toBe(vector.salt_pw_hex);
    const password = opaquePassword(salt, kdfId, vector.passphrase ?? '');
    expect(password.toHex()).toBe(vector.password_hex);
    const config = { context: opaqueContext(id, kdfId), stretch: stretchFor(vector) };
    const progress: number[] = [];
    expect(config.context.toHex()).toBe(vector.context_hex);

    const { request, state } = createRegistrationRequest(password, hexToBytes(vector.blind_registration_hex));
    expect(decodeBase64url(vector.registration_request)).toEqual(request);
    const registration = await finalizeRegistrationRequest(
      state,
      decodeBase64url(vector.registration_response),
      config.stretch,
      hexToBytes(vector.envelope_nonce_hex),
    );
    expect(decodeBase64url(vector.registration)).toEqual(registration.record);
    expect(registration.exportKey.toHex()).toBe(vector.p_hex);

    const { ke1, login } = generateKE1(
      password,
      hexToBytes(vector.blind_login_hex),
      hexToBytes(vector.client_nonce_hex),
      hexToBytes(vector.client_keyshare_seed_hex),
    );
    expect(decodeBase64url(vector.ke1)).toEqual(ke1);
    const loginStretch = stretchFor(vector, (fraction) => {
      progress.push(fraction);
    });
    const result = await generateKE3(login, decodeBase64url(vector.ke2), { ...config, stretch: loginStretch });
    expect(decodeBase64url(vector.ke3)).toEqual(result.ke3);
    expect(result.exportKey.toHex()).toBe(vector.p_hex);
    expect(progress.at(-1)).toBe(kdfId === KDF_SCRYPT ? 1 : undefined);
  });
});
