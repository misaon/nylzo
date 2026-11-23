import { beforeAll, describe, expect, it, vi } from 'vitest';

import { pauseOffscreen } from './offscreen.ts';

class FakeIntersectionObserver implements IntersectionObserver {
  static readonly created: FakeIntersectionObserver[] = [];
  readonly root = null;
  readonly rootMargin: string;
  readonly scrollMargin = '0px';
  readonly thresholds: readonly number[] = [];
  readonly observed = new Set<Element>();
  readonly #callback: IntersectionObserverCallback;

  constructor(callback: IntersectionObserverCallback, options?: IntersectionObserverInit) {
    this.#callback = callback;
    this.rootMargin = options?.rootMargin ?? '0px';
    FakeIntersectionObserver.created.push(this);
  }

  observe(target: Element): void {
    this.observed.add(target);
  }

  unobserve(target: Element): void {
    this.observed.delete(target);
  }

  disconnect(): void {
    this.observed.clear();
  }

  takeRecords(): IntersectionObserverEntry[] {
    return [];
  }

  report(target: Element, isIntersecting: boolean): void {
    const box = new DOMRect(0, 0, 100, 100);
    this.#callback(
      [
        {
          target,
          isIntersecting,
          intersectionRatio: isIntersecting ? 1 : 0,
          intersectionRect: box,
          boundingClientRect: box,
          rootBounds: null,
          time: 0,
        },
      ],
      this,
    );
  }
}

describe('pause offscreen', () => {
  beforeAll(() => {
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
  });

  it('pauses a section while it is away from the screen and shares one observer', () => {
    const first = document.createElement('section');
    const second = document.createElement('section');
    const detachFirst = pauseOffscreen(first);
    const detachSecond = pauseOffscreen(second);
    const [observer] = FakeIntersectionObserver.created;

    expect(FakeIntersectionObserver.created).toHaveLength(1);
    expect(observer?.rootMargin).toBe('25% 0px');
    observer?.report(first, false);
    expect(first.dataset['paused']).toBe('');
    observer?.report(first, true);
    expect(first.dataset['paused']).toBeUndefined();

    observer?.report(second, false);
    detachSecond?.();
    expect(second.dataset['paused']).toBeUndefined();
    expect(observer?.observed.has(second)).toBe(false);
    expect(observer?.observed.has(first)).toBe(true);
    detachFirst?.();
  });
});
