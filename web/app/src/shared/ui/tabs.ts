const ARROW_STEPS: ReadonlyMap<string, number> = new Map([
  ['ArrowLeft', -1],
  ['ArrowRight', 1],
]);

export function arrowTarget(key: string, index: number, count: number): number | undefined {
  const step = ARROW_STEPS.get(key);
  return step === undefined ? undefined : (index + step + count) % count;
}
