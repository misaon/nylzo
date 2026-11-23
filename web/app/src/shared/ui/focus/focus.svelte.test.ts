import { describe, expect, it } from 'vitest';

import { FocusTargets } from './focus.ts';

describe('focus targets', () => {
  it('keeps the element that replaced a target when the old one detaches', () => {
    const targets = new FocusTargets<'field'>();
    const first = document.createElement('input');
    const second = document.createElement('input');

    const detachFirst = targets.attach('field')(first);
    const detachSecond = targets.attach('field')(second);
    detachFirst?.();
    expect(targets.get('field')).toBe(second);

    detachSecond?.();
    expect(targets.get('field')).toBeUndefined();
  });
});
