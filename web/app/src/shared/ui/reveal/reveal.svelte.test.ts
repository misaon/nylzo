import { DetachedWindowAPI } from 'happy-dom';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { reveal } from './reveal.ts';

class FakeIntersectionObserver implements IntersectionObserver {
  static latest: FakeIntersectionObserver | undefined;
  readonly root = null;
  readonly rootMargin = '0px';
  readonly scrollMargin = '0px';
  readonly thresholds: readonly number[] = [];
  readonly observed = new Set<Element>();
  readonly #callback: IntersectionObserverCallback;

  constructor(callback: IntersectionObserverCallback) {
    this.#callback = callback;
    FakeIntersectionObserver.latest = this;
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

  report(
    target: Element,
    { visibleHeight, height }: { readonly visibleHeight: number; readonly height: number },
  ): void {
    this.#callback(
      [
        {
          target,
          isIntersecting: visibleHeight > 0,
          intersectionRatio: visibleHeight / height,
          intersectionRect: new DOMRect(0, 0, 100, visibleHeight),
          boundingClientRect: new DOMRect(0, 0, 100, height),
          rootBounds: null,
          time: 0,
        },
      ],
      this,
    );
  }
}

function observer(): FakeIntersectionObserver {
  const latest = FakeIntersectionObserver.latest;
  if (!latest) {
    throw new Error('no observer was created');
  }
  return latest;
}

function reducedMotion(matches: boolean): void {
  const environment: object = globalThis;
  if (!('happyDOM' in environment) || !(environment.happyDOM instanceof DetachedWindowAPI)) {
    throw new TypeError('these tests need the happy-dom environment');
  }
  environment.happyDOM.settings.device.prefersReducedMotion = matches ? 'reduce' : 'no-preference';
}

describe('reveal', () => {
  beforeAll(() => {
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
  });

  afterEach(() => {
    reducedMotion(false);
  });

  it('leaves an element alone that is on screen when first measured', () => {
    const element = document.createElement('section');
    const detach = reveal(element);

    observer().report(element, { visibleHeight: 300, height: 300 });

    expect(element.dataset['reveal']).toBeUndefined();
    expect(observer().observed.has(element)).toBe(false);
    detach?.();
  });

  it('reveals an element below the fold once enough of it scrolls into view', () => {
    const element = document.createElement('section');
    const detach = reveal(element);

    observer().report(element, { visibleHeight: 0, height: 400 });
    expect(element.dataset['reveal']).toBe('pending');
    observer().report(element, { visibleHeight: 50, height: 400 });
    expect(element.dataset['reveal']).toBe('pending');
    observer().report(element, { visibleHeight: 100, height: 400 });
    expect(element.dataset['reveal']).toBe('in');
    expect(observer().observed.has(element)).toBe(false);
    detach?.();
  });

  it('reveals a tall element after 200 pixels of it are visible', () => {
    const element = document.createElement('section');
    const detach = reveal(element);

    observer().report(element, { visibleHeight: 0, height: 2000 });
    observer().report(element, { visibleHeight: 200, height: 2000 });

    expect(element.dataset['reveal']).toBe('in');
    detach?.();
  });

  it('forgets an element when detached so a new attach measures it again', () => {
    const element = document.createElement('section');
    const detach = reveal(element);
    observer().report(element, { visibleHeight: 0, height: 400 });

    detach?.();
    expect(element.dataset['reveal']).toBeUndefined();
    expect(observer().observed.has(element)).toBe(false);

    const detachAgain = reveal(element);
    observer().report(element, { visibleHeight: 0, height: 400 });
    expect(element.dataset['reveal']).toBe('pending');
    detachAgain?.();
  });

  it('does not watch anything for reduced motion', () => {
    reducedMotion(true);
    const element = document.createElement('section');

    expect(reveal(element)).toBeUndefined();
    expect(FakeIntersectionObserver.latest?.observed.has(element) ?? false).toBe(false);
  });
});
