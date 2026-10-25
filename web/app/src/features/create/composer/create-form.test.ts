import { describe, expect, it } from 'vitest';

import type { InstanceLimits } from '../../../shared/api/responses.ts';

import { english } from '../../../shared/language/english.ts';
import { failureKey, problemKey, readAttachment, ttlChoices, ttlKey } from './create-form.ts';

const limits: InstanceLimits = {
  maxFileBytes: 16,
  maxTextBytes: 102_400,
  maxObjectBytes: 5_373_983,
  maxTtlSeconds: 604_800,
  defaultTtlSeconds: 86_400,
  maxOpens: 10,
};

describe('create form', () => {
  it('offers the default expiry of the instance even when it is not a preset', () => {
    expect(ttlChoices(limits)).toEqual([3600, 86_400, 259_200, 604_800]);
    expect(ttlChoices({ ...limits, defaultTtlSeconds: 7200 })).toEqual([3600, 7200, 86_400, 259_200, 604_800]);
    expect(ttlChoices({ ...limits, maxTtlSeconds: 86_400, defaultTtlSeconds: 86_400 })).toEqual([3600, 86_400]);
    expect(ttlChoices({ ...limits, defaultTtlSeconds: 1 })).toEqual([3600, 86_400, 259_200, 604_800]);
  });

  it('labels only the presets and leaves other expiries to the duration format', () => {
    expect(ttlKey(3600)).toBe('ttl.3600');
    expect(ttlKey(7200)).toBeUndefined();
  });

  it('maps every problem and failure to a translated text', () => {
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
    ];
    for (const key of keys) {
      expect(english[key], key).toBeTypeOf('string');
    }
    expect(failureKey({ kind: 'invalid', problems: [] })).toBe('failure.failed');
  });

  it('refuses a file over the limit before reading it', async () => {
    const big = new File([new Uint8Array(17)], 'big.bin');
    expect(await readAttachment(big, limits)).toEqual({ problem: { field: 'file', code: 'too_large' } });
    const small = new File([new Uint8Array([1, 2])], 'small.txt', { type: 'text/plain' });
    expect(await readAttachment(small, limits)).toEqual({
      file: { name: 'small.txt', mime: 'text/plain', bytes: new Uint8Array([1, 2]) },
    });
    expect(await readAttachment(undefined, limits)).toEqual({ file: undefined });
  });
});
