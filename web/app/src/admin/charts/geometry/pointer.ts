const PAGE_STEP = 7;

export function nearestIndex(position: number, start: number, width: number, count: number): number {
  if (count <= 1 || width <= 0) {
    return 0;
  }
  const fraction = (position - start) / width;
  return Math.min(count - 1, Math.max(0, Math.round(fraction * (count - 1))));
}

export function bandIndex(position: number, start: number, width: number, count: number): number {
  if (count <= 1 || width <= 0) {
    return 0;
  }
  return Math.min(count - 1, Math.max(0, Math.floor(((position - start) / width) * count)));
}

export function steppedIndex(key: string, index: number, count: number): number | undefined {
  const last = Math.max(0, count - 1);
  const moves: Readonly<Record<string, number>> = {
    ArrowLeft: index - 1,
    ArrowDown: index - 1,
    ArrowRight: index + 1,
    ArrowUp: index + 1,
    PageDown: index - PAGE_STEP,
    PageUp: index + PAGE_STEP,
    Home: 0,
    End: last,
  };
  const target = moves[key];
  return target === undefined ? undefined : Math.min(last, Math.max(0, target));
}
