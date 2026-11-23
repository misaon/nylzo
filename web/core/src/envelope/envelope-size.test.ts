import { describe, expect, it, vi } from 'vitest';

import { randomId } from '../../test/vectors.ts';
import { TooLargeError } from '../errors.ts';
import { deriveContent } from '../keys/derive.ts';
import { KDF_NONE } from '../keys/kdf.ts';
import { PAD } from '../limits.ts';
import { newSecret } from '../primitives/identifiers.ts';
import { seal } from './envelope.ts';
import { newHeader } from './header.ts';

vi.mock(import('../limits.ts'), async (importOriginal) => {
  const limits = await importOriginal();
  return { ...limits, MAX_OBJECT_BYTES: limits.PAD };
});

describe('seal bounds the object it builds', () => {
  it('refuses a message whose padded object exceeds MAX_OBJECT_BYTES', async () => {
    const id = randomId();
    const keys = await deriveContent({ id, secret: newSecret(), exportKey: new Uint8Array(64), share: newSecret() });
    const policy = { opensMax: 1, ttlSeconds: 86_400, deadline: 1_797_000_000 };
    const payload = { meta: { text: 'x'.repeat(PAD) } };
    await expect(seal(keys, newHeader(KDF_NONE), id, policy, payload)).rejects.toBeInstanceOf(TooLargeError);
  });
});
