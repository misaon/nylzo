import { flushSync, tick } from 'svelte';
import type { Attachment } from 'svelte/attachments';
import { prefersReducedMotion } from 'svelte/motion';

const GLIDE = { duration: 500, easing: 'cubic-bezier(0.22, 1, 0.36, 1)' } as const;

// The layout changes at once and the element travels with a transform, which no later layout shift can count.
export class Glide {
  readonly #elements = new Set<HTMLElement>();
  readonly #running = new WeakMap<HTMLElement, Animation>();
  #waiting = false;

  readonly attach: Attachment<HTMLElement> = (element) => {
    this.#elements.add(element);
    return () => {
      this.#elements.delete(element);
    };
  };

  // Changes made while a glide waits for the DOM join that glide, so it measures once, after all of them.
  async around(change: () => void): Promise<void> {
    if (this.#waiting) {
      change();
      return;
    }
    const before = [...this.#elements].map((element) => ({ element, top: element.getBoundingClientRect().top }));
    change();
    if (before.length === 0 || prefersReducedMotion.current) {
      return;
    }
    this.#waiting = true;
    try {
      await tick();
      flushSync();
    } finally {
      this.#waiting = false;
    }
    for (const { element, top } of before) {
      this.#running.get(element)?.cancel();
      const offset = top - element.getBoundingClientRect().top;
      if (offset !== 0) {
        this.#running.set(element, element.animate([{ translate: `0 ${offset}px` }, { translate: '0 0' }], GLIDE));
      }
    }
  }
}
