export const webFlow = { id: 19_864_447, login: 'web-flow' } as const;

export interface Account {
  readonly id: number;
  readonly login: string;
  readonly type: string;
}

export interface PullRequest {
  readonly author: Account;
  readonly baseBranch: string;
  readonly baseRepository: string;
  readonly commitCount: number;
  readonly draft: boolean;
  readonly head: string;
  readonly headRepository: string | null;
  readonly number: number;
}

export interface Commit {
  readonly author: Account | null;
  readonly committer: Account | null;
  readonly parents: readonly string[];
  readonly sha: string;
  readonly verified: boolean;
}

export interface Review {
  readonly commit: string | null;
  readonly reviewer: Account | null;
  readonly state: string;
}

export interface TreeEntry {
  readonly mode: string;
  readonly sha: string;
  readonly type: string;
}

export type Tree = ReadonlyMap<string, TreeEntry>;

export interface GuardSource {
  blob(sha: string): Promise<string>;
  branchHead(branch: string): Promise<string>;
  commits(pullRequest: number): Promise<readonly Commit[]>;
  pullRequest(pullRequest: number): Promise<PullRequest>;
  releaseAssetDigest(repository: string, tag: string, asset: string): Promise<string | undefined>;
  reviews(pullRequest: number): Promise<readonly Review[]>;
  tagCommit(repository: string, tag: string): Promise<string | undefined>;
  tree(commit: string): Promise<Tree>;
}

const regularFileModes = new Set(['100644', '100755']);

export function isRegularFile(entry: TreeEntry | undefined): entry is TreeEntry {
  return entry?.type === 'blob' && regularFileModes.has(entry.mode);
}

export async function readFileAt(source: GuardSource, commit: string, path: string): Promise<string | undefined> {
  const tree = await source.tree(commit);
  const entry = tree.get(path);
  return isRegularFile(entry) ? source.blob(entry.sha) : undefined;
}
