import { createHash } from 'node:crypto';

import type { BotIdentity } from '../github/identity.ts';
import type { Account, Commit, GuardSource, PullRequest, Review, TreeEntry } from './source.ts';

import { repositoryName } from '../github/client.ts';
import { webFlow } from './source.ts';

export interface FileVersion {
  readonly content: string;
  readonly mode?: string;
}

export type FileTree = Readonly<Record<string, FileVersion | string>>;

export interface FakeRepository {
  readonly branches: Readonly<Record<string, string>>;
  readonly commits: readonly Commit[];
  readonly pull: PullRequest;
  readonly releaseAssets: Readonly<Record<string, string>>;
  readonly reviews: readonly Review[];
  readonly snapshots: Readonly<Record<string, FileTree>>;
  readonly tags: Readonly<Record<string, string>>;
}

export interface FileChange {
  readonly after?: FileVersion | string;
  readonly before?: FileVersion | string;
  readonly path: string;
}

export const bot: BotIdentity = { id: 349_732_443, login: 'nylzo-dependency-updates[bot]' };
export const appAccount: Account = { ...bot, type: 'Bot' };
export const owner: Account = { id: 15_806_356, login: 'misaon', type: 'User' };
export const stranger: Account = { id: 1, login: 'octocat', type: 'User' };
export const gitHubWebFlow: Account = { ...webFlow, type: 'User' };
export const baseCommit = 'b'.repeat(40);
export const headCommit = 'c'.repeat(40);
export const codeOwnersFile = '* @misaon\n/.github/ @misaon\n';

function blobSha(content: string): string {
  return createHash('sha1').update(content).digest('hex');
}

function content(version: FileVersion | string): FileVersion {
  return typeof version === 'string' ? { content: version } : version;
}

export function fakeSource(repository: FakeRepository): GuardSource {
  const blobs = new Map<string, string>();
  for (const files of Object.values(repository.snapshots)) {
    for (const version of Object.values(files)) {
      blobs.set(blobSha(content(version).content), content(version).content);
    }
  }
  return {
    blob(sha) {
      const text = blobs.get(sha);
      return text === undefined ? Promise.reject(new Error(`No blob ${sha}`)) : Promise.resolve(text);
    },
    branchHead(branch) {
      const head = repository.branches[branch];
      return head === undefined ? Promise.reject(new Error(`No branch ${branch}`)) : Promise.resolve(head);
    },
    commits: () => Promise.resolve(repository.commits),
    pullRequest: () => Promise.resolve(repository.pull),
    releaseAssetDigest: (fullName, tag, asset) =>
      Promise.resolve(repository.releaseAssets[`${fullName}@${tag}/${asset}`]),
    reviews: () => Promise.resolve(repository.reviews),
    tagCommit: (fullName, tag) => Promise.resolve(repository.tags[`${fullName}@${tag}`]),
    tree(commit) {
      const files = repository.snapshots[commit];
      if (files === undefined) {
        return Promise.reject(new Error(`No commit ${commit}`));
      }
      const entries = Object.entries(files).map(([path, version]): [string, TreeEntry] => {
        const { content: text, mode = '100644' } = content(version);
        return [path, { mode, sha: blobSha(text), type: 'blob' }];
      });
      return Promise.resolve(new Map(entries));
    },
  };
}

export function appCommit(overrides: Partial<Commit> = {}): Commit {
  return {
    author: appAccount,
    committer: gitHubWebFlow,
    parents: [baseCommit],
    sha: headCommit,
    verified: true,
    ...overrides,
  };
}

export function dependencyUpdate(
  changes: readonly FileChange[],
  overrides: Partial<FakeRepository> = {},
): FakeRepository {
  const before: Record<string, FileVersion | string> = { '.github/CODEOWNERS': codeOwnersFile };
  const after: Record<string, FileVersion | string> = { '.github/CODEOWNERS': codeOwnersFile };
  for (const change of changes) {
    if (change.before !== undefined) {
      before[change.path] = change.before;
    }
    if (change.after !== undefined) {
      after[change.path] = change.after;
    }
  }
  return {
    branches: { main: baseCommit },
    commits: [appCommit()],
    pull: {
      author: appAccount,
      baseBranch: 'main',
      baseRepository: repositoryName,
      commitCount: 1,
      draft: false,
      head: headCommit,
      headRepository: repositoryName,
      number: 7,
    },
    releaseAssets: {},
    reviews: [],
    snapshots: { [baseCommit]: before, [headCommit]: after },
    tags: {},
    ...overrides,
  };
}
