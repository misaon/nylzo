declare module 'node:fs' {
  export function readFileSync(path: URL, encoding: 'utf8'): string;
}

declare const process: {
  readonly env: Readonly<Record<string, string | undefined>>;
};
