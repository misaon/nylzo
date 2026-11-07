import { z } from 'zod';

import { createGitHubClient } from '../github/client.ts';
import { maintenanceBranch, parseVersion } from '../versions/semantic-version.ts';
import { branchTip, latestPublishedRelease, unreleasedPullRequests } from './github-state.ts';

const hour = 60 * 60 * 1000;
const securityDelay = hour;
// Monday, Wednesday and Friday windows leave at most three days between releases; one more day covers a failed run.
const updateDelay = 4 * 24 * hour;

const client = createGitHubClient(z.string().min(1).parse(process.env['GITHUB_TOKEN']));
const latest = await latestPublishedRelease(client);
const branch = maintenanceBranch(parseVersion(latest.tag));
const tip = await branchTip(client, branch);

if (tip !== null && tip !== latest.commit) {
  const unreleased = await unreleasedPullRequests(client, latest.commit, tip, branch);
  const current = await latestPublishedRelease(client);
  const now = Date.now();
  const waitedLongerThan = (delay: number) => (pull: { readonly mergedAt: number }) => now - pull.mergedAt > delay;
  if (current.tag === latest.tag) {
    if (unreleased.filter((pull) => pull.labels.includes('security')).some(waitedLongerThan(securityDelay))) {
      throw new Error(`A security fix on ${branch} has waited more than an hour for its patch release.`);
    }
    if (unreleased.some(waitedLongerThan(updateDelay))) {
      throw new Error(
        `${branch} has waited more than four days for its dependency release. Inspect CI and the window.`,
      );
    }
  }
}
console.log('No maintenance change is waiting longer than its release allows.');
