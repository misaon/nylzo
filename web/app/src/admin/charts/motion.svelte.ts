import { quintOut } from 'svelte/easing';
import { Tween } from 'svelte/motion';

import { motionMs } from '../../shared/ui/transition/motion.ts';

const DURATION_MS = 900;

type Values = readonly number[];

interface Layers {
  readonly keys: Values | undefined;
  readonly layers: readonly Values[];
}

export interface SeriesOptions {
  readonly keys?: () => Values | undefined;
  readonly onlyLatestChanges?: boolean;
  readonly milliseconds?: number;
}

function fromTheRight(from: Values, to: Values): number[] {
  const shift = from.length - to.length;
  return to.map((_, i) => from[i + shift] ?? 0);
}

function byKey(fromKeys: Values, from: Values, toKeys: Values, to: Values): number[] | undefined {
  const known = new Map(fromKeys.map((key, i) => [key, from[i] ?? 0]));
  if (!toKeys.some((key) => known.has(key))) {
    return undefined;
  }
  const starts: number[] = [];
  for (const i of to.keys()) {
    const key = toKeys[i];
    starts.push((key === undefined ? undefined : known.get(key)) ?? starts[i - 1] ?? 0);
  }
  return starts;
}

// In a window where only the latest value still counts, the smallest shift that keeps the other values is how far it
// moved.
function bySlide(from: Values, to: Values): number[] | undefined {
  const last = to.length - 1;
  if (from.length !== to.length) {
    return undefined;
  }
  for (let shift = 0; shift <= last; shift++) {
    const kept = last - shift;
    if (to.slice(0, kept).every((value, i) => value === from[i + shift]) && (to[kept] ?? 0) >= (from[last] ?? 0)) {
      return to.map((_, i) => from[Math.min(i + shift, last)] ?? 0);
    }
  }
  return undefined;
}

function startsOf(from: Layers, to: Layers, onlyLatestChanges: boolean): number[][] {
  return to.layers.map((values, layer) => {
    const before = from.layers[layer] ?? [];
    const keyed =
      from.keys === undefined || to.keys === undefined ? undefined : byKey(from.keys, before, to.keys, values);
    return keyed ?? (onlyLatestChanges ? bySlide(before, values) : undefined) ?? fromTheRight(before, values);
  });
}

export function tweenedNumber(read: () => number, milliseconds = DURATION_MS): Tween<number> {
  return Tween.of(read, { easing: quintOut, duration: motionMs(milliseconds) });
}

export function tweenedLayers(
  read: () => readonly Values[],
  { keys, onlyLatestChanges = false, milliseconds = DURATION_MS }: SeriesOptions = {},
): { readonly current: readonly Values[] } {
  const tween = Tween.of<Layers>(() => ({ keys: keys?.(), layers: read() }), {
    easing: quintOut,
    duration: motionMs(milliseconds),
    interpolate: (from, to) => {
      const starts = startsOf(from, to, onlyLatestChanges);
      return (progress) => ({
        keys: to.keys,
        layers: starts.map((start, layer) =>
          start.map((value, i) => value + ((to.layers[layer]?.[i] ?? 0) - value) * progress),
        ),
      });
    },
  });
  return {
    get current() {
      return tween.current.layers;
    },
  };
}

export function tweenedSeries(read: () => Values, options?: SeriesOptions): { readonly current: Values } {
  const layers = tweenedLayers(() => [read()], options);
  return {
    get current() {
      return layers.current[0] ?? [];
    },
  };
}
