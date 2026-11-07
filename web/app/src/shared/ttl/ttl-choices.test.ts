import { describe, expect, it } from 'vitest';

import type { InstanceLimits } from '../api/responses.ts';
import { ttlChoices } from './ttl-choices.ts';

const limits: InstanceLimits = {
  maxFileBytes: 16,
  maxTextBytes: 102_400,
  maxObjectBytes: 5_373_952,
  maxTtlSeconds: 604_800,
  defaultTtlSeconds: 86_400,
  maxOpens: 10,
};

describe('expiry choices', () => {
  it('offers the default expiry of the instance even when it is not a preset', () => {
    expect(ttlChoices(limits)).toEqual([3600, 86_400, 259_200, 604_800]);
    expect(ttlChoices({ ...limits, defaultTtlSeconds: 7200 })).toEqual([3600, 7200, 86_400, 259_200, 604_800]);
    expect(ttlChoices({ ...limits, maxTtlSeconds: 86_400, defaultTtlSeconds: 86_400 })).toEqual([3600, 86_400]);
    expect(ttlChoices({ ...limits, defaultTtlSeconds: 1 })).toEqual([3600, 86_400, 259_200, 604_800]);
  });
});
