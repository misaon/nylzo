import { describe, expect, it } from 'vitest';

import { ApiError, endedStateOf } from './api-errors.ts';

describe('a gone message', () => {
  it('names the state it ended in, and an available message has none', () => {
    expect(endedStateOf(new ApiError(410, 'gone', { state: 'cancelled' }))).toBe('cancelled');
    expect(endedStateOf(new ApiError(410, 'gone', { state: 'available' }))).toBeUndefined();
  });
});
