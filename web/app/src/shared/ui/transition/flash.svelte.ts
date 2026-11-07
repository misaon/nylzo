import { onDestroy } from 'svelte';

export class Flash {
  #timer: ReturnType<typeof setTimeout> | undefined;
  readonly #durationMs: number;
  shown = $state(false);

  constructor(durationMs: number) {
    this.#durationMs = durationMs;
    onDestroy(() => clearTimeout(this.#timer));
  }

  hide(): void {
    clearTimeout(this.#timer);
    this.shown = false;
  }

  show(): void {
    this.shown = true;
    clearTimeout(this.#timer);
    this.#timer = setTimeout(() => {
      this.shown = false;
    }, this.#durationMs);
  }
}
