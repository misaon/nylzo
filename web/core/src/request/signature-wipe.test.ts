import type * as Sha2 from '@noble/hashes/sha2.js';

import { ed25519 } from '@noble/curves/ed25519.js';
import { describe, expect, it, vi } from 'vitest';

import { SEED_LENGTH } from '../limits.ts';
import { requestKey } from './signature.ts';

const digests = vi.hoisted((): Uint8Array[] => []);

vi.mock('@noble/hashes/sha2.js', async (importOriginal) => {
  const original = await importOriginal<typeof Sha2>();
  const recorded = (message: Uint8Array) => {
    const digest = original.sha512(message);
    digests.push(digest);
    return digest;
  };
  return { ...original, sha512: Object.assign(recorded, original.sha512) };
});

describe('seed-derived Ed25519 material (W2-09)', () => {
  it('overwrites the SHA-512 digest of a seed once its public key is derived', async () => {
    const seed = new Uint8Array(SEED_LENGTH).fill(7);
    const key = await requestKey(Uint8Array.from(seed));
    expect(key.publicKey).toEqual(ed25519.getPublicKey(seed));
    expect(digests).toHaveLength(1);
    expect(digests[0]?.every((byte) => byte === 0)).toBe(true);
  });
});
