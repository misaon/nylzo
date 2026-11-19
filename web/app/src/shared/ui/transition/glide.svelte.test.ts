import { describe, expect, it, vi } from 'vitest';

import { Glide } from './glide.ts';

describe('glide', () => {
  it('folds changes made while it waits into one glide from where the element was', async () => {
    const glide = new Glide();
    const element = document.createElement('div');
    let top = 100;
    vi.spyOn(element, 'getBoundingClientRect').mockImplementation(() => DOMRect.fromRect({ y: top }));
    const animate = vi.spyOn(element, 'animate').mockImplementation(() => new Animation());
    glide.attach(element);
    await Promise.all([
      glide.around(() => {
        top = 40;
      }),
      glide.around(() => {
        top = 30;
      }),
    ]);
    expect(animate).toHaveBeenCalledExactlyOnceWith([{ translate: '0 70px' }, { translate: '0 0' }], expect.anything());
  });
});
