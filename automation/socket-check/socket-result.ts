export type SocketResult = 'failure' | 'pending' | 'success';

export interface CheckRun {
  readonly app: { readonly id: bigint | number } | null;
  readonly conclusion: string | null;
  readonly head_sha: string;
  readonly id: bigint | number;
  readonly name: string;
  readonly status: string;
}

export const socketCheckName = 'Socket Security: Pull Request Alerts';

const socketAppId = 156_372;

export function socketResult(checks: readonly CheckRun[], head: string): SocketResult {
  const current = checks
    .filter((check) => check.name === socketCheckName && check.app?.id === socketAppId && check.head_sha === head)
    .toSorted((first, second) => Number(second.id) - Number(first.id))[0];
  if (current?.status !== 'completed') {
    return 'pending';
  }
  return current.conclusion === 'success' ? 'success' : 'failure';
}
