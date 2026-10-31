import { readFileSync } from 'node:fs';
import { z } from 'zod';

import { createGitHubClient, repository, repositoryName } from '../github/client.ts';
import { maintenanceBranch, parseVersion } from '../versions/semantic-version.ts';
import { branchTip, mergedPullRequest, taggedCommit } from './github-state.ts';

const thisRepository = z.object({ full_name: z.literal(repositoryName) });

const completedRelease = z.object({
  workflow_run: z.object({
    conclusion: z.literal('success'),
    event: z.literal('push'),
    head_branch: z.string(),
    head_repository: thisRepository,
    name: z.literal('release'),
  }),
});

const publishedRelease = z.object({
  draft: z.literal(false),
  immutable: z.literal(true),
  prerelease: z.literal(false),
});

const eventPath = z.string().parse(process.env['GITHUB_EVENT_PATH']);
const tag = completedRelease.parse(JSON.parse(readFileSync(eventPath, 'utf8'))).workflow_run.head_branch;
const version = parseVersion(tag);
const client = createGitHubClient(z.string().min(1).parse(process.env['GITHUB_TOKEN']));
const release = await client.rest.repos.getReleaseByTag({ ...repository, tag });
publishedRelease.parse(release.data);
const commit = await taggedCommit(client, tag);

if (version.patch === 0) {
  const branch = maintenanceBranch(version);
  if ((await branchTip(client, branch)) === null) {
    await client.rest.git.createRef({ ...repository, ref: `refs/heads/${branch}`, sha: commit });
    console.log(`Created the maintenance branch ${branch} at ${tag}.`);
  }
  const pull = await mergedPullRequest(client, commit, 'main');
  if (pull !== undefined) {
    await client.rest.issues.addLabels({ ...repository, issue_number: pull.number, labels: ['autorelease: tagged'] });
    if (pull.labels.some((label) => label.name === 'autorelease: pending')) {
      await client.rest.issues.removeLabel({ ...repository, issue_number: pull.number, name: 'autorelease: pending' });
    }
  }
}
console.log(`Recorded the immutable release ${tag}.`);
