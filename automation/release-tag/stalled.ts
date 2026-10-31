import { z } from 'zod';

import { createGitHubClient, repository } from '../github/client.ts';
import { maintenanceBranch, parseVersion } from '../versions/semantic-version.ts';
import { branchTip, latestPublishedRelease } from './github-state.ts';

const allowedDelayInMilliseconds = 60 * 60 * 1000;

const client = createGitHubClient(z.string().min(1).parse(process.env['GITHUB_TOKEN']));
const latest = await latestPublishedRelease(client);
const branch = maintenanceBranch(parseVersion(latest.tag));
const tip = await branchTip(client, branch);

if (tip !== null && tip !== latest.commit) {
  const commit = await client.rest.git.getCommit({ ...repository, commit_sha: tip });
  const waitingSince = Date.parse(commit.data.committer.date);
  const current = await latestPublishedRelease(client);
  if (current.tag === latest.tag && Date.now() - waitingSince > allowedDelayInMilliseconds) {
    throw new Error(`${branch} has waited more than an hour for its patch release. Inspect CI and the release queue.`);
  }
}
console.log('No maintenance commit is waiting for its release.');
