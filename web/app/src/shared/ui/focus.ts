import type { Attachment } from 'svelte/attachments';

import { tick } from 'svelte';

export function keepFocus(fallback: HTMLElement | undefined): void {
  const active = document.activeElement;
  const lost =
    !active ||
    active === document.body ||
    !active.isConnected ||
    (active instanceof HTMLButtonElement && active.disabled);
  if (lost) {
    fallback?.focus();
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

  async focusAfterUpdate(target: () => Target): Promise<void> {
    await tick();
    this.focus(target());
  }

  focus(target: Target): void {
    this.#elements[target]?.focus();
  }

  get(target: Target): HTMLElement | undefined {
    return this.#elements[target];
  }
}
