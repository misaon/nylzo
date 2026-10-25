export interface Gate {
  readonly opened: Promise<true>;
  readonly open: () => void;
}

export function newGate(): Gate {
  const { promise, resolve } = Promise.withResolvers<true>();
  return { opened: promise, open: () => resolve(true) };
}
