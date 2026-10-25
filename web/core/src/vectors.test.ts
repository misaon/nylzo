import { describe, expect, it } from 'vitest';

import { bytesToHex, hexToBytes, loadVectors } from '../test/vectors.ts';
import { encodeBase64url } from './primitives/base64url.ts';
import {
  authorizationHeader,
  parseChallenge,
  PURPOSE_VERIFIER,
  type RequestKey,
  requestKey,
  requestMessage,
  seedRequestKey,
  signRequest,
  verifierHeader,
} from './request/signature.ts';

const vectors = loadVectors();
describe('request', () => {
  it.each(vectors.request)('$name', async (vector) => {
    const seed = hexToBytes(vector.seed_hex);
    const key = await requestKey(seed);
    const fallback = seedRequestKey(seed);
    expect('privateKey' in key && key.privateKey.extractable).toBe(false);
    expect(encodeBase64url(key.publicKey)).toBe(vector.public_key);
    expect(encodeBase64url(fallback.publicKey)).toBe(vector.public_key);

    const challenge = parseChallenge(vector.challenge);
    const body = hexToBytes(vector.body_hex);
    const message = await requestMessage(vector.purpose, vector.method, vector.path, challenge, body);
    expect(bytesToHex(message)).toBe(vector.message_hex);

    const sign = (signingKey: RequestKey) =>
      signRequest(signingKey, vector.purpose, vector.method, vector.path, challenge, body);
    const signature = await sign(key);
    const fallbackSignature = await sign(fallback);
    expect(encodeBase64url(signature.signature)).toBe(vector.signature);
    expect(encodeBase64url(fallbackSignature.signature)).toBe(vector.signature);
    expect({ authorization: vector.authorization, verifier_header: vector.verifier_header }).toEqual(
      vector.purpose === PURPOSE_VERIFIER
        ? { verifier_header: verifierHeader(signature) }
        : { authorization: authorizationHeader(signature) },
    );
  });
});
