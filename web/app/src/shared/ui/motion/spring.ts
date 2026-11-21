export interface SpringSettings {
  readonly response: number;
  readonly dampingRatio: number;
}

export const SETTLED: SpringSettings = { response: 0.35, dampingRatio: 1 };
export const THROWN: SpringSettings = { response: 0.3, dampingRatio: 0.8 };

const STEP_SECONDS = 0.001;
const LONGEST_FRAME_SECONDS = 0.064;
const REST_DISTANCE = 0.5;
const REST_VELOCITY = 5;
const DECELERATION_RATE = 0.998;
const RUBBER_BAND = 0.55;

// Where a value released at this velocity (per second) comes to rest, as scrolling decelerates.
export function project(velocity: number): number {
  return ((velocity / 1000) * DECELERATION_RATE) / (1 - DECELERATION_RATE);
}

// The further past the edge, the less the value follows.
export function rubberBand(overshoot: number, dimension: number): number {
  return (overshoot * dimension * RUBBER_BAND) / (dimension + RUBBER_BAND * Math.abs(overshoot));
}

export class Spring {
  #frame: number | undefined;
  #last = 0;
  #target = 0;
  #settings = SETTLED;
  #onRest: (() => void) | undefined;
  readonly #apply: (value: number) => void;
  value: number;
  velocity = 0;

  constructor(value: number, apply: (value: number) => void) {
    this.value = value;
    this.#apply = apply;
  }

  get moving(): boolean {
    return this.#frame !== undefined;
  }

  set(value: number): void {
    this.stop();
    this.value = value;
    this.velocity = 0;
    this.#apply(value);
  }

  // A new target keeps the value and the velocity the spring has now, so a redirected motion never jumps.
  to(target: number, settings: SpringSettings, velocity?: number, onRest?: () => void): void {
    this.#target = target;
    this.#settings = settings;
    this.#onRest = onRest;
    if (velocity !== undefined) {
      this.velocity = velocity;
    }
    if (this.#frame === undefined) {
      this.#last = performance.now();
      this.#frame = requestAnimationFrame(this.#tick);
    }
  }

  stop(): void {
    if (this.#frame !== undefined) {
      cancelAnimationFrame(this.#frame);
      this.#frame = undefined;
    }
    this.#onRest = undefined;
  }

  readonly #tick = (now: number): void => {
    const elapsed = Math.min(LONGEST_FRAME_SECONDS, (now - this.#last) / 1000);
    this.#last = now;
    const frequency = (2 * Math.PI) / this.#settings.response;
    const stiffness = frequency * frequency;
    const friction = 2 * this.#settings.dampingRatio * frequency;
    for (let passed = 0; passed < elapsed; passed += STEP_SECONDS) {
      const acceleration = -stiffness * (this.value - this.#target) - friction * this.velocity;
      this.velocity += acceleration * STEP_SECONDS;
      this.value += this.velocity * STEP_SECONDS;
    }
    if (Math.abs(this.value - this.#target) < REST_DISTANCE && Math.abs(this.velocity) < REST_VELOCITY) {
      this.value = this.#target;
      this.velocity = 0;
      this.#frame = undefined;
      this.#apply(this.value);
      const onRest = this.#onRest;
      this.#onRest = undefined;
      onRest?.();
      return;
    }
    this.#apply(this.value);
    this.#frame = requestAnimationFrame(this.#tick);
  };
}
