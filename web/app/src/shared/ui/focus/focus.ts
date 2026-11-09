import { tick } from 'svelte';
import type { Attachment } from 'svelte/attachments';

// A plain focus() jumps the page to the element; this scrolls only when needed, with the page's smooth scrolling.
function focusGently(element: HTMLElement | undefined): void {
  element?.focus({ preventScroll: true });
  element?.scrollIntoView({ block: 'nearest' });
}

export function keepFocus(fallback: HTMLElement | undefined): void {
  const active = document.activeElement;
  const lost =
    !active ||
    active === document.body ||
    !active.isConnected ||
    (active instanceof HTMLButtonElement && active.disabled);
  if (lost) {
    focusGently(fallback);
  }
}

export class FocusTargets<Target extends string> {
  readonly #elements: Partial<Record<Target, HTMLElement | undefined>> = {};

  readonly attach =
    (target: Target): Attachment<HTMLElement> =>
    (element) => {
      this.#elements[target] = element;
      return () => {
        if (this.#elements[target] === element) {
          this.#elements[target] = undefined;
        }
      };
    };

  async focusAfterUpdate(target: Target | (() => Target)): Promise<void> {
    await tick();
    this.focus(typeof target === 'function' ? target() : target);
  }

  async keepFocusAfterUpdate(fallback: Target): Promise<void> {
    await tick();
    keepFocus(this.#elements[fallback]);
  }

  focus(target: Target): void {
    focusGently(this.#elements[target]);
  }

  get(target: Target): HTMLElement | undefined {
    return this.#elements[target];
  }
}
