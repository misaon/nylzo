import type { Attachment } from 'svelte/attachments';

import { prefersReducedMotion } from 'svelte/motion';

const TICK_MS = 50;
const VISIBLE_THRESHOLD = 0.35;

export class AutoCycle {
  #visible = $state(false);
  #pointerInside = $state(false);
  #focusInside = $state(false);
  #stopped = $state(false);
  readonly #count: () => number;
  readonly #duration: (index: number) => number;
  index = $state(0);
  progress = $state(0);

  readonly observe: Attachment<HTMLElement> = (element) => {
    const observer = new IntersectionObserver(
      (entries) => {
        this.#visible = entries.some((entry) => entry.isIntersecting);
      },
      { threshold: VISIBLE_THRESHOLD },
    );
    observer.observe(element);
    return () => observer.disconnect();
  };

  readonly advance: Attachment = () => {
    if (!this.running) {
      return;
    }
    const timer = setInterval(() => this.#tick(), TICK_MS);
    return () => clearInterval(timer);
  };

  readonly pointerEnter = (): void => {
    this.#pointerInside = true;
  };

  readonly pointerLeave = (): void => {
    this.#pointerInside = false;
  };

  readonly focusIn = (event: FocusEvent): void => {
    if (event.target instanceof Element && event.target.matches(':focus-visible')) {
      this.#focusInside = true;
    }
  };

  readonly focusOut = (event: FocusEvent): void => {
    const target = event.currentTarget;
    if (!(target instanceof Node && event.relatedTarget instanceof Node && target.contains(event.relatedTarget))) {
      this.#focusInside = false;
    }
  };

  constructor(count: () => number, duration: (index: number) => number) {
    this.#count = count;
    this.#duration = duration;
  }

  #tick(): void {
    const next = this.progress + TICK_MS / Math.max(TICK_MS, this.#duration(this.index));
    if (next < 1) {
      this.progress = next;
      return;
    }
    this.progress = 0;
    this.index = (this.index + 1) % Math.max(1, this.#count());
  }

  get running(): boolean {
    return (
      this.#visible && !this.#pointerInside && !this.#focusInside && !this.#stopped && !prefersReducedMotion.current
    );
  }

  select(index: number): void {
    this.index = index;
    this.progress = 0;
    this.#stopped = true;
  }
}
