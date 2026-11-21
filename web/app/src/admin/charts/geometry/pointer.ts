const PAGE_STEP = 7;

interface AxisPointer {
  readonly position: number;
  readonly start: number;
  readonly width: number;
  readonly count: number;
}

interface IndexStep {
  readonly key: string;
  readonly index: number;
  readonly count: number;
}

export function nearestIndex({ position, start, width, count }: AxisPointer): number {
  if (count <= 1 || width <= 0) {
    return 0;
  }
  const fraction = (position - start) / width;
  return Math.min(count - 1, Math.max(0, Math.round(fraction * (count - 1))));
}

export function bandIndex({ position, start, width, count }: AxisPointer): number {
  if (count <= 1 || width <= 0) {
    return 0;
  }
  return Math.min(count - 1, Math.max(0, Math.floor(((position - start) / width) * count)));
}

export function steppedIndex({ key, index, count }: IndexStep): number | undefined {
  const last = Math.max(0, count - 1);
  const moves: ReadonlyMap<string, number> = new Map([
    ['ArrowLeft', index - 1],
    ['ArrowDown', index - 1],
    ['ArrowRight', index + 1],
    ['ArrowUp', index + 1],
    ['PageDown', index - PAGE_STEP],
    ['PageUp', index + PAGE_STEP],
    ['Home', 0],
    ['End', last],
  ]);
  const target = moves.get(key);
  return target === undefined ? undefined : Math.min(last, Math.max(0, target));
}
