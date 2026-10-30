import { describe, expect, it, vi } from 'vitest';

import { bytesToHex, hexToBytes, vectors } from '../../test/vectors.ts';
import { SEED_LENGTH } from '../limits.ts';
import { encodeBase64url } from '../primitives/base64url.ts';
import { utf8 } from '../primitives/bytes.ts';
import {
  authorizationHeader,
  CHALLENGE_LENGTH,
  newRequestKey,
  parseChallenge,
  PURPOSE_MANAGE,
  PURPOSE_READ,
  PURPOSE_VERIFIER,
  type RequestKey,
  requestKey,
  requestMessage,
  signRequest,
  verifierHeader,
  wipeRequestKey,
} from './signature.ts';

const path = '/api/v1/messages/ye0eIOs8vLjsJSKaFFBFyg';

describe('AK9: request vectors', () => {
  it.each(vectors.request)('$name', async (vector) => {
    const seed = hexToBytes(vector.seed_hex);
    const key = await requestKey(seed);
    expect('privateKey' in key && key.privateKey.extractable).toBe(false);
    expect(encodeBase64url(key.publicKey)).toBe(vector.public_key);

    const challenge = parseChallenge(vector.challenge);
    const body = hexToBytes(vector.body_hex);
    const message = await requestMessage(vector.purpose, vector.method, vector.path, challenge, body);
    expect(bytesToHex(message)).toBe(vector.message_hex);

    const sign = (signingKey: RequestKey) =>
      signRequest(signingKey, vector.purpose, vector.method, vector.path, challenge, body);
    const signature = await sign(key);
    expect(encodeBase64url(signature.signature)).toBe(vector.signature);
    expect({ authorization: vector.authorization, verifier_header: vector.verifier_header }).toEqual(
      vector.purpose === PURPOSE_VERIFIER
        ? { verifier_header: verifierHeader(signature) }
        : { authorization: authorizationHeader(signature) },
    );
  });
});

describe('request signatures (spec 5.5)', () => {
  it('rejects a seed or challenge of the wrong length', async () => {
    await expect(requestKey(new Uint8Array(SEED_LENGTH - 1))).rejects.toThrow();
    const short = new Uint8Array(CHALLENGE_LENGTH - 1);
    await expect(requestMessage(PURPOSE_READ, 'GET', path, short, new Uint8Array(0))).rejects.toThrow();
  });

  it('generates a manage key whose private half never leaves Web Crypto (D39)', async () => {
    const key = await newRequestKey();
    expect(key.privateKey.extractable).toBe(false);
    expect(key.privateKey.usages).toEqual(['sign']);
    expect(key.publicKey).toHaveLength(32);
    await expect(crypto.subtle.exportKey('jwk', key.privateKey)).rejects.toThrow();
    const challenge = new Uint8Array(CHALLENGE_LENGTH).fill(3);
    const signature = await signRequest(key, PURPOSE_MANAGE, 'POST', path, challenge);
    const verifier = await crypto.subtle.importKey('raw', key.publicKey, 'Ed25519', false, ['verify']);
    const message = await requestMessage(PURPOSE_MANAGE, 'POST', path, challenge, new Uint8Array(0));
    expect(await crypto.subtle.verify('Ed25519', verifier, signature.signature, message)).toBe(true);
  });

  it.each([0, 1])('wipes the PKCS#8 copy of the seed when import %i fails', async (failAt) => {
    const imported: Uint8Array[] = [];
    const importKey = crypto.subtle.importKey.bind(crypto.subtle);
    const spy = vi.spyOn(crypto.subtle, 'importKey').mockImplementation(((
      format: KeyFormat,
      data: BufferSource,
      ...rest: [AlgorithmIdentifier, boolean, KeyUsage[]]
    ) => {
      if (format === 'pkcs8' && data instanceof Uint8Array) {
        imported.push(data);
        if (imported.length === failAt) {
          return Promise.reject(new Error('import failed'));
        }
      }
      return importKey(format as 'pkcs8', data, ...rest);
    }) as typeof crypto.subtle.importKey);
    let outcome = 'resolved';
    try {
      await requestKey(new Uint8Array(SEED_LENGTH).fill(7));
    } catch (error) {
      outcome = String(error);
    } finally {
      spy.mockRestore();
    }
    expect(outcome).toBe(failAt === 0 ? 'resolved' : 'Error: import failed');
    expect(imported.length).toBeGreaterThan(0);
    expect(imported.every((pkcs8) => pkcs8.every((byte) => byte === 0))).toBe(true);
  });

  it('uses a non-extractable JWK import when WebKit refuses a PKCS#8 seed, without exporting or signing in JavaScript', async () => {
    const seed = new Uint8Array(SEED_LENGTH).fill(7);
    seed[0] = 0;
    const native = await requestKey(seed);
    const importKey = crypto.subtle.importKey.bind(crypto.subtle);
    const imports: { format: KeyFormat; extractable: boolean }[] = [];
    const refuse = vi.spyOn(crypto.subtle, 'importKey').mockImplementation(((
      ...parameters: Parameters<typeof importKey>
    ) => {
      imports.push({ format: parameters[0], extractable: parameters[3] });
      return parameters[0] === 'pkcs8'
        ? Promise.reject(new DOMException('refused', 'DataError'))
        : importKey(...parameters);
    }) as typeof crypto.subtle.importKey);
    const exported = vi.spyOn(crypto.subtle, 'exportKey');
    try {
      const compatible = await requestKey(seed);
      expect(compatible.privateKey.extractable).toBe(false);
      expect('seed' in compatible).toBe(false);
      expect(compatible.publicKey).toEqual(native.publicKey);
      expect(imports).toEqual([
        { format: 'pkcs8', extractable: false },
        { format: 'jwk', extractable: false },
      ]);
      expect(exported).not.toHaveBeenCalled();
      const challenge = new Uint8Array(CHALLENGE_LENGTH).fill(5);
      const body = utf8('{"reason":"spam"}');
      const nativeSignature = await signRequest(native, PURPOSE_READ, 'POST', '/api/v1/abuse', challenge, body);
      const compatibleSignature = await signRequest(compatible, PURPOSE_READ, 'POST', '/api/v1/abuse', challenge, body);
      expect(compatibleSignature.signature).toEqual(nativeSignature.signature);
      seed.fill(9);
      wipeRequestKey(compatible);
      await expect(signRequest(compatible, PURPOSE_READ, 'POST', '/api/v1/abuse', challenge, body)).rejects.toThrow(
        'released',
      );
    } finally {
      refuse.mockRestore();
      exported.mockRestore();
    }
  });

  it('fails closed if neither native private-key import succeeds', async () => {
    const refuse = vi.spyOn(crypto.subtle, 'importKey').mockRejectedValue(new DOMException('refused', 'DataError'));
    try {
      await expect(requestKey(new Uint8Array(SEED_LENGTH).fill(7))).rejects.toMatchObject({ name: 'DataError' });
    } finally {
      refuse.mockRestore();
    }
  });

  it('keeps other import errors', async () => {
    const refuse = vi.spyOn(crypto.subtle, 'importKey').mockRejectedValue(new DOMException('no', 'NotSupportedError'));
    try {
      await expect(requestKey(new Uint8Array(SEED_LENGTH).fill(1))).rejects.toMatchObject({
        name: 'NotSupportedError',
      });
    } finally {
      refuse.mockRestore();
    }
  });

  it('tries generateKey again when it fails, as WebKit on Linux sometimes does', async () => {
    const generate = crypto.subtle.generateKey.bind(crypto.subtle);
    let calls = 0;
    const flaky = vi.spyOn(crypto.subtle, 'generateKey').mockImplementation(((
      ...arguments_: Parameters<typeof generate>
    ) => {
      calls++;
      return calls <= 2 ? Promise.reject(new DOMException('flaky', 'OperationError')) : generate(...arguments_);
    }) as typeof crypto.subtle.generateKey);
    try {
      const key = await newRequestKey();
      expect(calls).toBe(3);
      expect(key.privateKey.extractable).toBe(false);
    } finally {
      flaky.mockRestore();
    }
    const broken = vi
      .spyOn(crypto.subtle, 'generateKey')
      .mockRejectedValue(new DOMException('broken', 'OperationError'));
    try {
      await expect(newRequestKey()).rejects.toMatchObject({ name: 'OperationError' });
      expect(broken).toHaveBeenCalledTimes(8);
    } finally {
      broken.mockRestore();
    }
  });
});
