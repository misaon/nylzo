import { type Pace, pace } from './pipeline.ts';

const TICK_MS = 60;

export interface TimelineSettings {
  readonly minMs: number;
  readonly steps: number;
}

export class PacedTimeline {
  #elapsedMs = $state(0);
  #startedAt = 0;
  #timer: ReturnType<typeof setInterval> | undefined;
  readonly #settings: () => TimelineSettings;
  reached = $state(0);
  fraction = $state<number | undefined>();

  constructor(settings: () => TimelineSettings) {
    this.#settings = settings;
  }

  get paced(): Pace {
    return pace({ elapsedMs: this.#elapsedMs, ...this.#settings(), reached: this.reached, fraction: this.fraction });
  }

  complete(): void {
    this.reached = this.#settings().steps;
    this.fraction = undefined;
  }

  start(onTick: () => void): void {
    this.#startedAt = performance.now();
    this.#elapsedMs = 0;
    this.reached = 0;
    this.fraction = undefined;
    clearInterval(this.#timer);
    this.#timer = setInterval(() => {
      this.#elapsedMs = performance.now() - this.#startedAt;
      onTick();
    }, TICK_MS);
  }

  stop(): void {
    clearInterval(this.#timer);
    this.#timer = undefined;
  }
}
