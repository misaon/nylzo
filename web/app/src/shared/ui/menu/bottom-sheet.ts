import { prefersReducedMotion } from 'svelte/motion';

import { project, rubberBand, SETTLED, Spring, THROWN } from '../motion/spring.ts';

const DIRECTION_THRESHOLD = 10;
const VELOCITY_WINDOW_MS = 100;
const THROW_VELOCITY = 300;
const DISMISS_FRACTION = 0.5;

interface Sample {
  readonly time: number;
  readonly position: number;
}

interface Press {
  readonly pointerId: number;
  readonly x: number;
  readonly y: number;
  grabOffset: number | undefined;
}

function editable(target: EventTarget | null): boolean {
  return target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement;
}

function insideScrolledList(target: EventTarget | null, sheet: HTMLElement): boolean {
  for (let node = target instanceof Element ? target : null; node && node !== sheet; node = node.parentElement) {
    if (node.scrollHeight > node.clientHeight && getComputedStyle(node).overflowY !== 'visible') {
      return true;
    }
  }
  return false;
}

export class BottomSheet {
  readonly #element: HTMLElement;
  readonly #ondismiss: () => void;
  readonly #spring: Spring;
  readonly #removers: (() => void)[] = [];
  #samples: Sample[] = [];
  #press: Press | undefined;
  #open = false;
  #height = 0;
  #dragged = false;

  constructor(element: HTMLElement, ondismiss: () => void) {
    this.#element = element;
    this.#ondismiss = ondismiss;
    this.#spring = new Spring(0, this.#place);
    const listen = <Name extends keyof HTMLElementEventMap>(
      name: Name,
      handler: (event: HTMLElementEventMap[Name]) => void,
      capture = false,
    ) => {
      element.addEventListener(name, handler, capture);
      this.#removers.push(() => element.removeEventListener(name, handler, capture));
    };
    listen('pointerdown', this.#onPointerDown);
    listen('pointermove', this.#onPointerMove);
    listen('pointerup', this.#onPointerUp);
    listen('pointercancel', this.#onPointerUp);
    listen('click', this.#onClick, true);
    const viewport = globalThis.visualViewport;
    if (viewport) {
      viewport.addEventListener('resize', this.#aboveKeyboard);
      viewport.addEventListener('scroll', this.#aboveKeyboard);
      this.#removers.push(() => {
        viewport.removeEventListener('resize', this.#aboveKeyboard);
        viewport.removeEventListener('scroll', this.#aboveKeyboard);
      });
    }
  }

  setOpen(open: boolean): void {
    if (open === this.#open) {
      return;
    }
    this.#open = open;
    if (open) {
      this.#show();
    } else {
      this.#hide();
    }
  }

  destroy(): void {
    this.#spring.stop();
    for (const remove of this.#removers) {
      remove();
    }
    if (this.#element.matches(':popover-open')) {
      this.#element.hidePopover();
    }
    this.#element.style.removeProperty('translate');
    this.#element.style.removeProperty('bottom');
    this.#element.style.removeProperty('--sheet-shown');
  }

  #show(): void {
    const wasShown = this.#element.matches(':popover-open');
    if (!wasShown) {
      this.#element.showPopover();
    }
    this.#aboveKeyboard();
    this.#height = this.#element.offsetHeight;
    if (prefersReducedMotion.current) {
      this.#spring.set(0);
      return;
    }
    if (!wasShown) {
      this.#spring.set(this.#height);
    }
    this.#spring.to(0, SETTLED);
  }

  #hide(): void {
    const close = () => {
      if (!this.#open && this.#element.matches(':popover-open')) {
        this.#element.hidePopover();
      }
    };
    if (prefersReducedMotion.current) {
      this.#spring.set(this.#height);
      close();
      return;
    }
    this.#spring.to(this.#height, SETTLED, undefined, close);
  }

  readonly #place = (offset: number): void => {
    this.#element.style.translate = `0 ${offset}px`;
    const shown = this.#height === 0 ? 0 : 1 - Math.min(1, Math.max(0, offset / this.#height));
    this.#element.style.setProperty('--sheet-shown', shown.toFixed(3));
  };

  readonly #aboveKeyboard = (): void => {
    const viewport = globalThis.visualViewport;
    const covered = viewport ? Math.max(0, innerHeight - viewport.height - viewport.offsetTop) : 0;
    this.#element.style.bottom = `${covered}px`;
  };

  // oxlint-disable-next-line typescript/prefer-readonly-parameter-types -- the DOM hands a listener its own event type.
  readonly #onPointerDown = (event: PointerEvent): void => {
    if (!this.#open || !event.isPrimary || event.button !== 0 || editable(event.target)) {
      return;
    }
    if (insideScrolledList(event.target, this.#element)) {
      return;
    }
    this.#press = { pointerId: event.pointerId, x: event.clientX, y: event.clientY, grabOffset: undefined };
    this.#samples = [];
    this.#dragged = false;
  };

  // oxlint-disable-next-line typescript/prefer-readonly-parameter-types -- the DOM hands a listener its own event type.
  readonly #onPointerMove = (event: PointerEvent): void => {
    const press = this.#press;
    if (press?.pointerId !== event.pointerId) {
      return;
    }
    if (press.grabOffset === undefined) {
      const down = event.clientY - press.y;
      const across = event.clientX - press.x;
      if (Math.abs(down) < DIRECTION_THRESHOLD || Math.abs(down) < Math.abs(across)) {
        return;
      }
      // The sheet is taken where it is now, mid-flight included, so it never jumps under the finger.
      this.#spring.stop();
      this.#height = this.#element.offsetHeight;
      press.grabOffset = event.clientY - this.#spring.value;
      this.#element.setPointerCapture(event.pointerId);
      this.#dragged = true;
    }
    const raw = event.clientY - press.grabOffset;
    const offset = raw >= 0 ? raw : -rubberBand(-raw, this.#height);
    this.#spring.set(offset);
    this.#samples.push({ time: event.timeStamp, position: offset });
    while (this.#samples.length > 2 && event.timeStamp - (this.#samples[0]?.time ?? 0) > VELOCITY_WINDOW_MS) {
      this.#samples.shift();
    }
  };

  // oxlint-disable-next-line typescript/prefer-readonly-parameter-types -- the DOM hands a listener its own event type.
  readonly #onPointerUp = (event: PointerEvent): void => {
    const press = this.#press;
    if (press?.pointerId !== event.pointerId) {
      return;
    }
    this.#press = undefined;
    if (press.grabOffset === undefined) {
      return;
    }
    const velocity = this.#releaseVelocity();
    const settings = Math.abs(velocity) > THROW_VELOCITY ? THROWN : SETTLED;
    const rest = this.#spring.value + project(velocity);
    if (event.type === 'pointerup' && rest > this.#height * DISMISS_FRACTION) {
      this.#open = false;
      this.#spring.to(this.#height, SETTLED, velocity, () => {
        if (!this.#open && this.#element.matches(':popover-open')) {
          this.#element.hidePopover();
        }
      });
      this.#ondismiss();
      return;
    }
    this.#spring.to(0, settings, velocity);
  };

  #releaseVelocity(): number {
    const first = this.#samples[0];
    const last = this.#samples.at(-1);
    if (!first || !last || last.time === first.time) {
      return 0;
    }
    return ((last.position - first.position) / (last.time - first.time)) * 1000;
  }

  readonly #onClick = (event: MouseEvent): void => {
    if (this.#dragged) {
      this.#dragged = false;
      event.preventDefault();
      event.stopPropagation();
      return;
    }
    if (event.target !== this.#element) {
      return;
    }
    const box = this.#element.getBoundingClientRect();
    if (
      event.clientY < box.top ||
      event.clientY > box.bottom ||
      event.clientX < box.left ||
      event.clientX > box.right
    ) {
      event.stopPropagation();
      this.#ondismiss();
    }
  };
}
