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

interface Span {
  readonly start: number;
  readonly end: number;
}

function fromTheRight(from: Values, to: Values): Span[] {
  const shift = from.length - to.length;
  return to.map((end, i) => ({ start: from[i + shift] ?? 0, end }));
}

interface Keyed {
  readonly keys: Values;
  readonly values: Values;
}

function byKey(from: Keyed, to: Keyed): Span[] | undefined {
  const known = new Map(from.keys.map((key, i) => [key, from.values[i] ?? 0]));
  if (!to.keys.some((key) => known.has(key))) {
    return undefined;
  }
  const spans: Span[] = [];
  for (const [i, end] of to.values.entries()) {
    const key = to.keys[i];
    spans.push({ start: (key === undefined ? undefined : known.get(key)) ?? spans[i - 1]?.start ?? 0, end });
  }
  return spans;
}

// In a window where only the latest value still counts, the smallest shift that keeps the other values is how far it
// moved.
function bySlide(from: Values, to: Values): Span[] | undefined {
  const latest = from.at(-1);
  if (from.length !== to.length || latest === undefined) {
    return undefined;
  }
  const last = to.length - 1;
  const kept = to.findLastIndex(
    (value, index) => value >= latest && to.slice(0, index).every((earlier, i) => earlier === from[i + last - index]),
  );
  if (kept === -1) {
    return undefined;
  }
  const shift = last - kept;
  return to.map((end, i) => ({ start: from[i + shift] ?? latest, end }));
}

function spansOf(from: Layers, to: Layers, onlyLatestChanges: boolean): Span[][] {
  return to.layers.map((values, layer) => {
    const before = from.layers[layer] ?? [];
    const keyed =
      from.keys === undefined || to.keys === undefined
        ? undefined
        : byKey({ keys: from.keys, values: before }, { keys: to.keys, values });
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
      const spans = spansOf(from, to, onlyLatestChanges);
      return (progress) => ({
        keys: to.keys,
        layers: spans.map((layer) => layer.map(({ start, end }) => start + (end - start) * progress)),
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
      return layers.current.flat();
    },
  };
}
