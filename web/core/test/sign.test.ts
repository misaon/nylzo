import { describe, expect, it, vi } from 'vitest';

import {
  CHALLENGE_LEN,
  PURPOSE_MANAGE,
  PURPOSE_READ,
  SIGNATURE_LEN,
  TOKEN_LEN,
  encodeBase64url,
  newRequestKey,
  requestKey,
  requestMessage,
  signRequest,
  utf8,
  verifierHeader,
} from '../src/index.ts';

const path = '/api/v1/messages/ye0eIOs8vLjsJSKaFFBFyg';

describe('request signatures', () => {
  it('rejects a seed or challenge of the wrong length', async () => {
    await expect(requestKey(new Uint8Array(TOKEN_LEN - 1))).rejects.toThrow();
    const short = new Uint8Array(CHALLENGE_LEN - 1);
    await expect(requestMessage(PURPOSE_READ, 'GET', path, short, new Uint8Array(0))).rejects.toThrow();
  });

  it('binds purpose, method, path, challenge and body', async () => {
    const challenge = new Uint8Array(CHALLENGE_LEN).fill(1);
    const other = new Uint8Array(CHALLENGE_LEN);
    const body = utf8('{"reason":"spam"}');
    const base = await requestMessage(PURPOSE_READ, 'GET', path, challenge, body);
    const variants = [
      await requestMessage(PURPOSE_MANAGE, 'GET', path, challenge, body),
      await requestMessage(PURPOSE_READ, 'POST', path, challenge, body),
      await requestMessage(PURPOSE_READ, 'GET', `${path}/blob`, challenge, body),
      await requestMessage(PURPOSE_READ, 'GET', path, other, body),
      await requestMessage(PURPOSE_READ, 'GET', path, challenge, utf8('{"reason":"spa"}')),
    ];
    for (const variant of variants) {
      expect(variant).not.toEqual(base);
    }
  });

  it('signs with a non-extractable key and formats the verifier header', async () => {
    const seed = new Uint8Array(TOKEN_LEN).fill(7);
    const key = await requestKey(seed);
    expect(key.privateKey.extractable).toBe(false);
    const sig = await signRequest(key, PURPOSE_READ, 'POST', `${path}/claim`, new Uint8Array(CHALLENGE_LEN));
    expect(sig.signature).toHaveLength(SIGNATURE_LEN);
    expect(verifierHeader(sig)).toBe(`${encodeBase64url(key.publicKey)}.${encodeBase64url(sig.signature)}`);
  });

  it('generates a manage key whose private half never leaves WebCrypto', async () => {
    const key = await newRequestKey();
    expect(key.privateKey.extractable).toBe(false);
    expect(key.privateKey.usages).toEqual(['sign']);
    expect(key.publicKey).toHaveLength(32);
    await expect(crypto.subtle.exportKey('jwk', key.privateKey)).rejects.toThrow();
    const challenge = new Uint8Array(CHALLENGE_LEN).fill(3);
    const signature = await signRequest(key, PURPOSE_MANAGE, 'POST', path, challenge);
    const verifier = await crypto.subtle.importKey('raw', key.publicKey, 'Ed25519', false, ['verify']);
    const message = await requestMessage(PURPOSE_MANAGE, 'POST', path, challenge, new Uint8Array(0));
    expect(await crypto.subtle.verify('Ed25519', verifier, signature.signature, message)).toBe(true);
    expect(encodeBase64url((await newRequestKey()).publicKey)).not.toBe(encodeBase64url(key.publicKey));
  });

  it('wipes the PKCS#8 copy of the seed it imports, also when an import fails', async () => {
    for (const failAt of [0, 1, 2]) {
      const imported: Uint8Array[] = [];
      const importKey = crypto.subtle.importKey.bind(crypto.subtle);
      const spy = vi.spyOn(crypto.subtle, 'importKey').mockImplementation(((format: KeyFormat, data: BufferSource, ...rest: [AlgorithmIdentifier, boolean, KeyUsage[]]) => {
        if (format === 'pkcs8' && data instanceof Uint8Array) {
          imported.push(data);
          if (imported.length === failAt) {
            return Promise.reject(new Error('import failed'));
          }
        }
        return importKey(format as 'pkcs8', data, ...rest);
      }) as typeof crypto.subtle.importKey);
      try {
        const result = requestKey(new Uint8Array(TOKEN_LEN).fill(7));
        await (failAt === 0 ? expect(result).resolves.toBeDefined() : expect(result).rejects.toThrow('import failed'));
      } finally {
        spy.mockRestore();
      }
      expect(imported.length, `fail at ${failAt}`).toBeGreaterThan(0);
      expect(imported.every((pkcs8) => pkcs8.every((byte) => byte === 0)), `fail at ${failAt}`).toBe(true);
    }
  });
});
