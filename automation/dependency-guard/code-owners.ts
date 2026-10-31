import type { Account, Review } from './source.ts';

export type CodeOwners = ReadonlySet<string>;

const userOwner = /^@(?<login>[\dA-Z](?:[\dA-Z-]*[\dA-Z])?)$/i;
const deciding = new Set(['APPROVED', 'CHANGES_REQUESTED', 'DISMISSED']);

function lineOwners(line: string): string[] {
  const [pattern, ...entries] = line.trim().split(/\s+/);
  if (pattern === undefined || pattern === '' || pattern.startsWith('#')) {
    return [];
  }
  const comment = entries.findIndex((entry) => entry.startsWith('#'));
  return entries
    .slice(0, comment === -1 ? entries.length : comment)
    .map((entry) => userOwner.exec(entry)?.groups?.['login']?.toLowerCase())
    .filter((login) => login !== undefined);
}

export function parseCodeOwners(text: string): CodeOwners {
  return new Set(text.split('\n').flatMap((line) => lineOwners(line)));
}

export function isCodeOwner(account: Account | null, owners: CodeOwners): boolean {
  return account?.type === 'User' && owners.has(account.login.toLowerCase());
}

export function approvingCodeOwner(reviews: readonly Review[], owners: CodeOwners, head: string): string | undefined {
  const decisions = new Map<number, Review>();
  for (const review of reviews) {
    if (review.reviewer !== null && deciding.has(review.state) && isCodeOwner(review.reviewer, owners)) {
      decisions.set(review.reviewer.id, review);
    }
  }
  let approver: string | undefined;
  for (const review of decisions.values()) {
    if (review.state === 'CHANGES_REQUESTED') {
      return undefined;
    }
    if (review.state === 'APPROVED' && review.commit === head) {
      approver = review.reviewer?.login;
    }
  }
  return approver;
}
