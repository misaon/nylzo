import { ristretto255 } from '@noble/curves/ed25519.js';
import type { ristretto255_hasher as Hasher } from '@noble/curves/ed25519.js';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { OpaqueMessageError } from '../errors.ts';
import { createRegistrationRequest, deriveKeyPair, INFO_DIFFIE_HELLMAN } from './opaque.ts';

const hasher = vi.hoisted(() => ({
  hashToScalar: vi.fn<typeof Hasher.hashToScalar>(),
  hashToCurve: vi.fn<typeof Hasher.hashToCurve>(),
}));

vi.mock(import('@noble/curves/ed25519.js'), async (importOriginal) => {
  const curves = await importOriginal();
  return { ...curves, ristretto255_hasher: { ...curves.ristretto255_hasher, ...hasher } };
});

beforeEach(async () => {
  const { ristretto255_hasher: original } =
    await vi.importActual<typeof import('@noble/curves/ed25519.js')>('@noble/curves/ed25519.js');
  hasher.hashToScalar.mockReset().mockImplementation((...input) => original.hashToScalar(...input));
  hasher.hashToCurve.mockReset().mockImplementation((...input) => original.hashToCurve(...input));
});

describe('inputs that RFC 9497 maps onto degenerate group values', () => {
  it('stops DeriveKeyPair after 256 zero scalars', () => {
    hasher.hashToScalar.mockReturnValue(0n);
    expect(() => deriveKeyPair(new Uint8Array(32), INFO_DIFFIE_HELLMAN)).toThrow(OpaqueMessageError);
    expect(hasher.hashToScalar).toHaveBeenCalledTimes(256);
  });

  it('refuses an input that hashes to the identity element', () => {
    hasher.hashToCurve.mockReturnValue(ristretto255.Point.ZERO);
    expect(() => createRegistrationRequest(new Uint8Array([1]))).toThrow(OpaqueMessageError);
  });
});
