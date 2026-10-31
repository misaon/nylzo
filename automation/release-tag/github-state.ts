import { z } from 'zod';

import { type GitHubClient, repository } from '../github/client.ts';
import { peelTag } from '../github/tags.ts';

const latestRelease = z.object({ draft: z.literal(false), immutable: z.literal(true), tag_name: z.string() });

export type PullRequest = Awaited<
  ReturnType<GitHubClient['rest']['repos']['listPullRequestsAssociatedWithCommit']>
>['data'][number];

export interface PublishedRelease {
  readonly commit: string;
  readonly tag: string;
}

export async function latestPublishedRelease(client: GitHubClient): Promise<PublishedRelease> {
  const response = await client.rest.repos.getLatestRelease(repository);
  const release = latestRelease.parse(response.data);
  return { commit: await taggedCommit(client, release.tag_name), tag: release.tag_name };
}

export async function taggedCommit(client: GitHubClient, tag: string): Promise<string> {
  const reference = await client.rest.git.getRef({ ...repository, ref: `tags/${tag}` });
  if (reference.data.object.type !== 'tag') {
    throw new Error(`${tag} must be an annotated tag`);
  }
  const target = await peelTag(client, repository, reference.data.object, 1);
  if (target.type !== 'commit') {
    throw new Error(`${tag} must point at a commit`);
  }
  return target.sha;
}

export async function tagExists(client: GitHubClient, tag: string): Promise<boolean> {
  try {
    await client.rest.git.getRef({ ...repository, ref: `tags/${tag}` });
    return true;
  } catch (error) {
    if (error instanceof Error && 'status' in error && error.status === 404) {
      return false;
    }
    throw error;
  }
}

export async function branchTip(client: GitHubClient, branch: string): Promise<string | null> {
  try {
    const response = await client.rest.git.getRef({ ...repository, ref: `heads/${branch}` });
    return response.data.object.sha;
  } catch (error) {
    if (error instanceof Error && 'status' in error && error.status === 404) {
      return null;
    }
    throw error;
  }
}

export async function mergedPullRequest(
  client: GitHubClient,
  commit: string,
  branch: string,
): Promise<PullRequest | undefined> {
  const response = await client.rest.repos.listPullRequestsAssociatedWithCommit({ ...repository, commit_sha: commit });
  return response.data.find(
    (pull) => pull.merged_at !== null && pull.merge_commit_sha === commit && pull.base.ref === branch,
  );
}
