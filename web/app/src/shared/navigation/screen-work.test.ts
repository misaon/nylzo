import { describe, expect, it } from 'vitest';

import { ScreenWork } from './screen-work.ts';

describe('the work of a screen', () => {
  it('is idle until a screen watches its work, and idle again once that screen stops', () => {
    const work = new ScreenWork();
    expect(work.busy).toBe(false);
    const stop = work.watch(() => true);
    expect(work.busy).toBe(true);
    stop();
    expect(work.busy).toBe(false);
  });

  it('keeps the work of the next screen when the previous one stops late', () => {
    const work = new ScreenWork();
    const stopPrevious = work.watch(() => false);
    work.watch(() => true);
    stopPrevious();
    expect(work.busy).toBe(true);
  });
});
