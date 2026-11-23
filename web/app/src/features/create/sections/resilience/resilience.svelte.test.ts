import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { getTranslator, Translator } from '../../../../shared/language/translator.svelte.ts';
import { countdown, judge, yearsText } from './brute-force.ts';
import Resilience from './Resilience.svelte';

vi.mock(import('../../../../shared/language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);

type Entries = readonly Pick<IntersectionObserverEntry, 'isIntersecting'>[];

describe('brute force section', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;
  const translator = new Translator('en');
  const callbacks = new Map<Element, (entries: Entries) => void>();

  class RecordingObserver {
    readonly #callback: (entries: Entries) => void;

    constructor(callback: (entries: Entries) => void) {
      this.#callback = callback;
    }

    observe(element: Element): void {
      callbacks.set(element, this.#callback);
    }

    unobserve(element: Element): void {
      callbacks.delete(element);
    }

    disconnect(): void {
      for (const [element, callback] of callbacks) {
        if (callback === this.#callback) {
          callbacks.delete(element);
        }
      }
    }
  }

  function counter(): (entries: Entries) => void {
    const panel = [...callbacks.keys()].find((element) => element.querySelector('.grid-cols-7') !== null);
    const callback = panel && callbacks.get(panel);
    if (!callback) {
      throw new Error('missing countdown panel');
    }
    return callback;
  }
  function done(): string {
    return target.querySelector('.tabular-nums.text-ink[aria-hidden="true"]')?.textContent ?? '';
  }
  function verdict(): string {
    return target.querySelector('[aria-live="polite"]')?.textContent ?? '';
  }
  function attacker(id: string): HTMLInputElement {
    const radio = target.querySelector<HTMLInputElement>(`input[type="radio"][value="${id}"]`);
    if (!radio) {
      throw new Error(`missing attacker ${id}`);
    }
    return radio;
  }
  function quantumSwitch(): HTMLButtonElement {
    const found = target.querySelector<HTMLButtonElement>('button[role="switch"]');
    if (!found) {
      throw new Error('missing quantum switch');
    }
    return found;
  }

  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(getTranslator).mockReturnValue(translator);
    callbacks.clear();
    vi.stubGlobal('IntersectionObserver', RecordingObserver);
    target = document.createElement('div');
    document.body.append(target);
    component = mount(Resilience, { target });
    flushSync();
  });
  afterEach(async () => {
    await unmount(component);
    target.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('counts down only while the countdown is on screen', () => {
    expect(done()).toBe(countdown('laptop', false, 0).done);
    counter()([{ isIntersecting: true }]);
    counter()([{ isIntersecting: true }]);
    vi.advanceTimersByTime(1000);
    flushSync();
    expect(done()).toBe(countdown('laptop', false, 1000).done);
    expect(target.querySelectorAll('.grid-cols-7 > .text-aegis-200').length).toBeGreaterThan(2);
    counter()([]);
    vi.advanceTimersByTime(1000);
    flushSync();
    expect(done()).toBe(countdown('laptop', false, 1000).done);
  });

  it('restarts the countdown and the verdict for another attacker and for a quantum computer', () => {
    counter()([{ isIntersecting: true }]);
    vi.advanceTimersByTime(500);
    attacker('world').dispatchEvent(new Event('change', { bubbles: true }));
    flushSync();
    expect(attacker('world').checked).toBe(true);
    expect(done()).toBe(countdown('world', false, 0).done);
    expect(verdict()).toContain(translator.translate('brute.bigbang'));
    expect(target.querySelector('[aria-live="polite"] > div')?.className).toBe('animate-wave');
    quantumSwitch().click();
    flushSync();
    expect(quantumSwitch().getAttribute('aria-checked')).toBe('true');
    expect(verdict()).toContain(translator.translate('brute.verdict.earth'));
    const years = yearsText(judge('world', true).yearsLog10, translator);
    expect(verdict()).toContain(translator.translate('brute.about', { time: years }));
    expect(target.textContent).toContain(translator.translate('brute.left.steps'));
    expect(done()).toBe(countdown('world', true, 0).done);
  });
});
