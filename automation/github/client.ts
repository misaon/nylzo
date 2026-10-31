import { Octokit } from '@octokit/core';
import { paginateRest } from '@octokit/plugin-paginate-rest';
import { restEndpointMethods } from '@octokit/plugin-rest-endpoint-methods';
import { setTimeout as sleep } from 'node:timers/promises';

const GitHub = Octokit.plugin(paginateRest, restEndpointMethods);

export type GitHubClient = InstanceType<typeof GitHub>;

export const repository = { owner: 'misaon', repo: 'nylzo' } as const;

export const repositoryName = `${repository.owner}/${repository.repo}`;

const retryDelaysInMilliseconds = [2000, 8000, 30_000];

export function createGitHubClient(token: string): GitHubClient {
  const client = new GitHub({ auth: token, userAgent: 'nylzo-automation' });
  client.hook.wrap('request', async (request, options) => {
    for (const delay of retryDelaysInMilliseconds) {
      try {
        return await request(options);
      } catch (error) {
        if (!isServerError(error)) {
          throw error;
        }
        await sleep(delay);
      }
    }
    return request(options);
  });
  return client;
}

function isServerError(error: unknown): boolean {
  return error instanceof Error && 'status' in error && typeof error.status === 'number' && error.status >= 500;
}
