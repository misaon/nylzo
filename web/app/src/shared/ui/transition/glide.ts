import { tick } from 'svelte';
import type { Attachment } from 'svelte/attachments';
import { prefersReducedMotion } from 'svelte/motion';

const GLIDE = { duration: 500, easing: 'cubic-bezier(0.22, 1, 0.36, 1)' } as const;

// The layout changes at once and the element travels with a transform, which no later layout shift can count.
export class Glide {
  #element: HTMLElement | undefined;

  readonly attach: Attachment<HTMLElement> = (element) => {
    this.#element = element;
    return () => {
      if (this.#element === element) {
        this.#element = undefined;
      }
    };
  };

  async around(change: () => void): Promise<void> {
    const element = this.#element;
    const top = element?.getBoundingClientRect().top;
    change();
    if (element === undefined || top === undefined || prefersReducedMotion.current) {
      return;
    }
    await tick();
    const offset = top - element.getBoundingClientRect().top;
    if (offset !== 0) {
      element.animate([{ translate: `0 ${offset}px` }, { translate: '0 0' }], GLIDE);
    }
  }
}
