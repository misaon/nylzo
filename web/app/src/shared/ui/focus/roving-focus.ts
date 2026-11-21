const ARROW_STEPS: ReadonlyMap<string, number> = new Map([
  ['ArrowLeft', -1],
  ['ArrowRight', 1],
]);

interface Position {
  readonly index: number;
  readonly count: number;
}

export function arrowTarget(key: string, { index, count }: Position): number | undefined {
  const step = ARROW_STEPS.get(key);
  return step === undefined ? undefined : (index + step + count) % count;
}
