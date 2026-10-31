import { z } from 'zod';

import type { Commit, GuardSource, PullRequest, Review, Tree, TreeEntry } from './source.ts';

import { type GitHubClient, repository } from '../github/client.ts';

const account = z.object({ id: z.number(), login: z.string(), type: z.string() });
const sha = z.string().regex(/^[a-f\d]{40}$/);
const repositoryReference = z.object({ full_name: z.string() });
const gitObject = z.object({ sha, type: z.string() });
const verification = z.object({ verified: z.boolean() });

const pullRequestSchema = z.object({
  base: z.object({ ref: z.string(), repo: repositoryReference }),
  commits: z.number(),
  draft: z.boolean().optional(),
  head: z.object({ repo: repositoryReference.nullable(), sha }),
  number: z.number(),
  user: account,
});

const commitSchema = z.object({
  author: account.nullable(),
  commit: z.object({ verification: verification.optional() }),
  committer: account.nullable(),
  parents: z.array(z.object({ sha })),
  sha,
});

const reviewSchema = z.object({ commit_id: z.string().nullable(), state: z.string(), user: account.nullable() });
const referenceSchema = z.object({ object: gitObject });
const treeEntry = z.object({ mode: z.string(), path: z.string(), sha, type: z.string() });
const treeSchema = z.object({ tree: z.array(treeEntry), truncated: z.boolean() });
const blobSchema = z.object({ content: z.string(), encoding: z.literal('base64') });
const releaseAsset = z.object({ digest: z.string().nullable(), name: z.string() });
const releaseSchema = z.object({ assets: z.array(releaseAsset) });

const utf8 = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });
const maximumTagDepth = 3;

function isMissing(error: unknown): boolean {
  return error instanceof Error && 'status' in error && error.status === 404;
}

async function unlessMissing<T>(request: Promise<T>): Promise<T | undefined> {
  try {
    return await request;
  } catch (error) {
    if (isMissing(error)) {
      return undefined;
    }
    throw error;
  }
}

function upstream(fullName: string): { owner: string; repo: string } {
  const [owner = '', repository_ = '', ...rest] = fullName.split('/');
  if (owner === '' || repository_ === '' || rest.length > 0) {
    throw new Error(`Not a repository name: ${fullName}`);
  }
  return { owner, repo: repository_ };
}

export function createGitHubSource(client: GitHubClient): GuardSource {
  const trees = new Map<string, Promise<Tree>>();

  async function loadTree(commit: string): Promise<Tree> {
    const response = await client.rest.git.getTree({ ...repository, recursive: 'true', tree_sha: commit });
    const { tree, truncated } = treeSchema.parse(response.data);
    if (truncated) {
      throw new Error(`The tree of ${commit} is too large to inspect`);
    }
    return new Map(
      tree.map(({ mode, path, sha: entrySha, type }): [string, TreeEntry] => [path, { mode, sha: entrySha, type }]),
    );
  }

  return {
    async blob(blobSha) {
      const response = await client.rest.git.getBlob({ ...repository, file_sha: blobSha });
      const { content } = blobSchema.parse(response.data);
      return utf8.decode(Buffer.from(content, 'base64'));
    },

    async branchHead(branch) {
      const response = await client.rest.git.getRef({ ...repository, ref: `heads/${branch}` });
      return referenceSchema.parse(response.data).object.sha;
    },

    async commits(pullRequest) {
      const commits = await client.paginate(client.rest.pulls.listCommits, {
        ...repository,
        per_page: 100,
        pull_number: pullRequest,
      });
      return z
        .array(commitSchema)
        .parse(commits)
        .map((commit): Commit => ({
          author: commit.author,
          committer: commit.committer,
          parents: commit.parents.map((parent) => parent.sha),
          sha: commit.sha,
          verified: commit.commit.verification?.verified === true,
        }));
    },

    async pullRequest(pullRequest) {
      const response = await client.rest.pulls.get({ ...repository, pull_number: pullRequest });
      const pull = pullRequestSchema.parse(response.data);
      return {
        author: pull.user,
        baseBranch: pull.base.ref,
        baseRepository: pull.base.repo.full_name,
        commitCount: pull.commits,
        draft: pull.draft === true,
        head: pull.head.sha,
        headRepository: pull.head.repo?.full_name ?? null,
        number: pull.number,
      } satisfies PullRequest;
    },

    async releaseAssetDigest(fullName, tag, asset) {
      const response = await unlessMissing(client.rest.repos.getReleaseByTag({ ...upstream(fullName), tag }));
      if (response === undefined) {
        return;
      }
      const release = releaseSchema.parse(response.data);
      return release.assets.find((candidate) => candidate.name === asset)?.digest ?? undefined;
    },

    async reviews(pullRequest) {
      const reviews = await client.paginate(client.rest.pulls.listReviews, {
        ...repository,
        per_page: 100,
        pull_number: pullRequest,
      });
      return z
        .array(reviewSchema)
        .parse(reviews)
        .map((review): Review => ({ commit: review.commit_id, reviewer: review.user, state: review.state }));
    },

    async tagCommit(fullName, tag) {
      const owner = upstream(fullName);
      const reference = await unlessMissing(client.rest.git.getRef({ ...owner, ref: `tags/${tag}` }));
      if (reference === undefined) {
        return;
      }
      let target = referenceSchema.parse(reference.data).object;
      for (let depth = 0; target.type === 'tag' && depth < maximumTagDepth; depth++) {
        const annotated = await client.rest.git.getTag({ ...owner, tag_sha: target.sha });
        target = referenceSchema.parse(annotated.data).object;
      }
      return target.type === 'commit' ? target.sha : undefined;
    },

    tree(commit) {
      let tree = trees.get(commit);
      if (tree === undefined) {
        tree = loadTree(commit);
        trees.set(commit, tree);
      }
      return tree;
    },
  };
}
