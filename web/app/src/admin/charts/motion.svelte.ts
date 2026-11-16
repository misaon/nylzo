import { quintOut } from 'svelte/easing';
import { Tween } from 'svelte/motion';

import { motionMs } from '../../shared/ui/transition/motion.ts';

const DURATION_MS = 900;

// A series of another length is a window that moved: the points keep their place from the right, so only the
// newest point grows in.
function between(from: readonly number[], to: readonly number[]): (progress: number) => number[] {
  const shift = from.length - to.length;
  const start = to.map((_, i) => from[i + shift] ?? 0);
  return (progress) => start.map((value, i) => value + ((to[i] ?? 0) - value) * progress);
}

export function tweenedNumber(read: () => number, milliseconds = DURATION_MS): Tween<number> {
  return Tween.of(read, { easing: quintOut, duration: motionMs(milliseconds) });
}

export function tweenedSeries(read: () => readonly number[], milliseconds = DURATION_MS): Tween<number[]> {
  return Tween.of(() => [...read()], { easing: quintOut, duration: motionMs(milliseconds), interpolate: between });
}
