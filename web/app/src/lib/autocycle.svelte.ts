import type { Attachment } from 'svelte/attachments';
import { prefersReducedMotion } from 'svelte/motion';

const TICK_MS = 50;
const RESUME_MS = 15_000;
const VISIBLE_THRESHOLD = 0.35;

export class AutoCycle {
  index = $state(0);
  progress = $state(0);
  visible = $state(false);
  pointer = $state(false);
  focused = $state(false);
  held = $state(false);

  readonly #count: () => number;
  readonly #duration: (index: number) => number;
  #resume: ReturnType<typeof setTimeout> | undefined;

  constructor(count: () => number, duration: (index: number) => number) {
    this.#count = count;
    this.#duration = duration;
  }

  get running(): boolean {
    return this.visible && !this.pointer && !this.focused && !this.held && !prefersReducedMotion.current;
  }

  select(index: number): void {
    this.index = index;
    this.progress = 0;
    this.held = true;
    clearTimeout(this.#resume);
    this.#resume = setTimeout(() => {
      this.held = false;
    }, RESUME_MS);
  }

  readonly pointerEnter = (): void => {
    this.pointer = true;
  };

  readonly pointerLeave = (): void => {
    this.pointer = false;
  };

  readonly focusIn = (event: FocusEvent): void => {
    if (event.target instanceof Element && event.target.matches(':focus-visible')) {
      this.focused = true;
    }
  };

  readonly focusOut = (event: FocusEvent): void => {
    const target = event.currentTarget;
    if (!(target instanceof Node && event.relatedTarget instanceof Node && target.contains(event.relatedTarget))) {
      this.focused = false;
    }
  };

  readonly observe: Attachment<HTMLElement> = (element) => {
    const observer =
      typeof IntersectionObserver === 'undefined'
        ? undefined
        : new IntersectionObserver((entries) => {
            this.visible = entries.some((entry) => entry.isIntersecting);
          }, { threshold: VISIBLE_THRESHOLD });
    if (observer) {
      observer.observe(element);
    } else {
      this.visible = true;
    }

    $effect(() => {
      if (!this.running) {
        return;
      }
      const timer = setInterval(() => this.#tick(), TICK_MS);
      return () => clearInterval(timer);
    });

    return () => {
      observer?.disconnect();
      clearTimeout(this.#resume);
    };
  };

  #tick(): void {
    const next = this.progress + TICK_MS / Math.max(TICK_MS, this.#duration(this.index));
    if (next < 1) {
      this.progress = next;
      return;
    }
    this.progress = 0;
    this.index = (this.index + 1) % Math.max(1, this.#count());
  }
}
