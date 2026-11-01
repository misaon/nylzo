import { describe, expect, it } from 'vitest';

import type { InstanceLimits } from '../../../shared/api/responses.ts';

import { readAttachment, ttlChoices } from './create-form.ts';

const limits: InstanceLimits = {
  maxFileBytes: 16,
  maxTextBytes: 102_400,
  maxObjectBytes: 5_373_952,
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

  it('refuses a file over the limit before reading it', async () => {
    const tooLarge = new File([new Uint8Array(17)], 'big.bin');
    expect(await readAttachment(tooLarge, limits)).toEqual({ problem: { field: 'file', code: 'too_large' } });
    const small = new File([new Uint8Array([1, 2])], 'small.txt', { type: 'text/plain' });
    expect(await readAttachment(small, limits)).toEqual({
      file: { name: 'small.txt', mime: 'text/plain', bytes: new Uint8Array([1, 2]) },
    });
    expect(await readAttachment(undefined, limits)).toEqual({ file: undefined });
  });

  it('reports a file it cannot read, such as a dropped folder, instead of failing the submit (A-03)', async () => {
    const folder = {
      name: 'folder',
      size: 0,
      arrayBuffer: () => Promise.reject(new DOMException('', 'NotFoundError')),
    };
    expect(await readAttachment(folder as unknown as File, limits)).toEqual({
      problem: { field: 'file', code: 'unreadable' },
    });
  });
});
