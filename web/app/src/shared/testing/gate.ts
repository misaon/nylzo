export interface Gate {
  readonly opened: Promise<void>;
  readonly open: () => void;
}

export function newGate(): Gate {
  const { promise, resolve } = Promise.withResolvers<undefined>();
  return { opened: promise, open: () => resolve(undefined) };
}
