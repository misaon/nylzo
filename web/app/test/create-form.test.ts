import { KDF_SCRYPT } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import type { InstanceLimits } from '../src/lib/api.ts';
import { failureKey, problemKey, progressKey, readAttachment, ttlChoices, ttlKey } from '../src/lib/create-form.ts';
import { dictionaries } from '../src/lib/i18n.ts';

const limits: InstanceLimits = {
  maxFileBytes: 16,
  maxTextBytes: 102400,
  maxObjectBytes: 5373983,
  maxTtlSeconds: 604800,
  defaultTtlSeconds: 86400,
  maxOpens: 10,
  defaultKdfId: KDF_SCRYPT,
};

describe('create form', () => {
  it('offers the default expiry of the instance even when it is not a preset', () => {
    expect(ttlChoices(limits)).toEqual([3600, 86400, 259200, 604800]);
    expect(ttlChoices({ ...limits, defaultTtlSeconds: 7200 })).toEqual([3600, 7200, 86400, 259200, 604800]);
    expect(ttlChoices({ ...limits, maxTtlSeconds: 86400, defaultTtlSeconds: 86400 })).toEqual([3600, 86400]);
    expect(ttlChoices({ ...limits, defaultTtlSeconds: 1 })).toEqual([3600, 86400, 259200, 604800]);
  });

  it('labels only the presets and leaves other expiries to the duration format', () => {
    expect(ttlKey(3600)).toBe('ttl.3600');
    expect(ttlKey(7200)).toBeUndefined();
  });

  it('maps every problem, failure and step to a translated text', () => {
    const keys = [
      problemKey({ field: 'content', code: 'missing' }),
      problemKey({ field: 'text', code: 'too_large' }),
      problemKey({ field: 'file', code: 'too_large' }),
      problemKey({ field: 'ttl', code: 'out_of_range' }),
      problemKey({ field: 'opens', code: 'out_of_range' }),
      problemKey({ field: 'passphrase', code: 'too_short' }),
      problemKey({ field: 'passphrase', code: 'malformed' }),
      failureKey({ kind: 'pow' }),
      failureKey({ kind: 'invalid', problems: [] }),
      progressKey({ step: 'proving', fraction: 0.5 }),
    ];
    for (const key of keys) {
      expect(dictionaries.en[key], key).toBeTypeOf('string');
    }
    expect(failureKey({ kind: 'invalid', problems: [] })).toBe('failure.failed');
  });

  it('refuses a file over the limit before reading it', async () => {
    const big = new File([new Uint8Array(17)], 'big.bin');
    expect(await readAttachment(big, limits)).toEqual({ problem: { field: 'file', code: 'too_large' } });
    const small = new File([new Uint8Array([1, 2])], 'small.txt', { type: 'text/plain' });
    expect(await readAttachment(small, limits)).toEqual({ file: { name: 'small.txt', mime: 'text/plain', bytes: new Uint8Array([1, 2]) } });
    expect(await readAttachment(undefined, limits)).toEqual({ file: undefined });
  });
});
