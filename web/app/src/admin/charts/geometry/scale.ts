export interface Ticks {
  readonly max: number;
  readonly values: readonly number[];
}

const STEP_FACTORS = [1, 2, 5, 10] as const;

export function niceTicks(maximum: number, count = 4): Ticks {
  if (!Number.isFinite(maximum) || maximum <= 0) {
    return { max: 1, values: [0] };
  }
  const raw = maximum / count;
  const magnitude = 10 ** Math.floor(Math.log10(raw));
  const factor = STEP_FACTORS.find((candidate) => candidate * magnitude >= raw) ?? 10;
  const step = Math.max(1, factor * magnitude);
  const top = Math.ceil(maximum / step) * step;
  const values = Array.from({ length: Math.round(top / step) + 1 }, (_, i) => i * step);
  return { max: top, values };
}

function intensity(value: number, maximum: number): number {
  if (value <= 0 || maximum <= 0) {
    return 0;
  }
  return Math.min(1, Math.sqrt(value / maximum));
}

export function level(value: number, maximum: number, steps: number): number {
  if (value <= 0 || maximum <= 0) {
    return 0;
  }
  return 1 + Math.min(steps - 1, Math.floor(intensity(value, maximum) * steps));
}

export function groupSeries(values: readonly number[], size: number): number[] {
  const width = Math.max(1, Math.floor(size));
  const groups: number[] = [];
  for (const [i, value] of values.entries()) {
    const group = Math.floor(i / width);
    groups[group] = (groups[group] ?? 0) + value;
  }
  return groups;
}

export function sum(values: readonly number[]): number {
  let total = 0;
  for (const value of values) {
    total += value;
  }
  return total;
}

export function share(value: number, total: number): number {
  return total > 0 ? value / total : 0;
}
