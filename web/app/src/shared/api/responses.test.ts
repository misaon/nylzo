import { describe, expect, it } from 'vitest';

import { ResponseError } from './api-errors.ts';
import { deadlineMatchesExpiry, messageState } from './responses.ts';

describe('server answers', () => {
  it('refuse a message state the protocol does not name', () => {
    expect(() => messageState({ state: 'archived' })).toThrow(ResponseError);
  });

  it('match a deadline only against an expiry the metadata reported', () => {
    const policy = { opensMax: 1, ttlSeconds: 3600, kdfId: 3, deadline: 1_797_000_000 };
    expect(deadlineMatchesExpiry(policy, new Date(1_797_000_000_000).toISOString())).toBe(true);
    expect(deadlineMatchesExpiry(policy, undefined)).toBe(false);
  });
});
