import { flushSync } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { AutoCycle } from '../src/lib/autocycle.svelte.ts';

const COUNT = 3;
const DURATION_MS = 1000;
const STEP_MS = DURATION_MS + 50;

function runCycle(): { readonly cycle: AutoCycle; readonly stop: () => void } {
  const cycle = new AutoCycle(
    () => COUNT,
    () => DURATION_MS,
  );
  const stop = $effect.root(() => cycle.observe({} as HTMLElement));
  flushSync();
  return { cycle, stop };
}

function focusOn(keyboard: boolean): FocusEvent {
  const target = Object.assign(new Element(), { matches: (selector: string) => keyboard && selector === ':focus-visible' });
  return { target } as unknown as FocusEvent;
}

function reducedMotion(matches: boolean): void {
  const query = window.matchMedia('(prefers-reduced-motion: reduce)') as MediaQueryList & { matches: boolean };
  query.matches = matches;
  query.dispatchEvent(new Event('change'));
  flushSync();
}

describe('AutoCycle', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    vi.useRealTimers();
    reducedMotion(false);
  });

  it('advances on its own and wraps around', () => {
    const { cycle, stop } = runCycle();
    vi.advanceTimersByTime(STEP_MS);
    expect(cycle.index).toBe(1);
    vi.advanceTimersByTime(2 * STEP_MS);
    expect(cycle.index).toBe(0);
    stop();
  });

  it('pauses while the pointer or the keyboard focus is inside', () => {
    const { cycle, stop } = runCycle();
    cycle.pointerEnter();
    flushSync();
    vi.advanceTimersByTime(STEP_MS);
    expect(cycle.index).toBe(0);
    cycle.focusIn(focusOn(true));
    cycle.pointerLeave();
    flushSync();
    vi.advanceTimersByTime(STEP_MS);
    expect(cycle.index).toBe(0);
    cycle.focusOut(new Event('focusout') as FocusEvent);
    flushSync();
    vi.advanceTimersByTime(STEP_MS);
    expect(cycle.index).toBe(1);
    stop();
  });

  it('holds a manual pick for 15 s and then moves on, even when the click focused it', () => {
    const { cycle, stop } = runCycle();
    cycle.focusIn(focusOn(false));
    cycle.select(2);
    flushSync();
    vi.advanceTimersByTime(14_000);
    expect(cycle.index).toBe(2);
    vi.advanceTimersByTime(1_000);
    flushSync();
    vi.advanceTimersByTime(STEP_MS);
    expect(cycle.index).toBe(0);
    stop();
  });

  it('stays put while the reader prefers reduced motion', () => {
    reducedMotion(true);
    const { cycle, stop } = runCycle();
    vi.advanceTimersByTime(STEP_MS);
    expect(cycle.index).toBe(0);
    reducedMotion(false);
    vi.advanceTimersByTime(STEP_MS);
    expect(cycle.index).toBe(1);
    stop();
  });

  it('stops its timer when it is detached', () => {
    const { cycle, stop } = runCycle();
    stop();
    vi.advanceTimersByTime(STEP_MS);
    expect(cycle.index).toBe(0);
  });
});
