import { appendFileSync, readFileSync } from 'node:fs';
import { z } from 'zod';

import { createGitHubClient, type GitHubClient, repository, repositoryName } from '../github/client.ts';
import { automationBot, isBot } from '../github/identity.ts';
import {
  branchTip,
  latestPublishedRelease,
  mergedPullRequest,
  type PublishedRelease,
  tagExists,
} from './github-state.ts';
import { maintenanceRelease, plannedRelease, type ReleasePlan } from './release-plan.ts';

const releasePullRequestBranch = 'release-please--branches--main';
const releasePullRequestFiles = new Set(['.github/CHANGELOG.md', '.release-please-manifest.json']);

const commitHash = z.string().regex(/^[\da-f]{40}$/);
const thisRepository = z.object({ full_name: z.literal(repositoryName) });

const completedRun = z.object({
  workflow_run: z.object({
    conclusion: z.literal('success'),
    event: z.literal('push'),
    head_branch: z.string(),
    head_repository: thisRepository,
    head_sha: commitHash,
    name: z.literal('ci'),
  }),
});

const manifest = z.object({ '.': z.string() }).strict();

async function plannedReleaseFromMain(
  client: GitHubClient,
  commit: string,
  latest: PublishedRelease,
): Promise<ReleasePlan | null> {
  const pull = await mergedPullRequest(client, commit, 'main');
  if (pull?.head.ref !== releasePullRequestBranch) {
    return null;
  }
  if (!isBot(pull.user, automationBot()) || pull.head.repo.full_name !== repositoryName) {
    throw new Error(`Release PR #${pull.number} must come from the automation App`);
  }
  const files = await client.paginate(client.rest.pulls.listFiles, { ...repository, pull_number: pull.number });
  if (files.some((file) => !releasePullRequestFiles.has(file.filename) || file.status === 'removed')) {
    throw new Error(`Release PR #${pull.number} may change only the manifest and the changelog`);
  }
  const content = await client.rest.repos.getContent({
    ...repository,
    path: '.release-please-manifest.json',
    ref: commit,
    mediaType: { format: 'raw' },
  });
  const version = manifest.parse(JSON.parse(z.string().parse(content.data)))['.'];
  if (pull.title !== `chore(main): release ${version}`) {
    throw new Error(`Release PR #${pull.number} title does not match the manifest version ${version}`);
  }
  return plannedRelease({ commit, latestRelease: latest.tag, manifestVersion: version });
}

async function maintenanceReleaseFromBranch(
  client: GitHubClient,
  branch: string,
  commit: string,
  latest: PublishedRelease,
): Promise<ReleasePlan | null> {
  if ((await branchTip(client, branch)) !== commit) {
    console.log(`${branch} moved past ${commit}; the newer commit releases after its own CI.`);
    return null;
  }
  if ((await mergedPullRequest(client, commit, branch)) === undefined) {
    console.log(`${commit} on ${branch} is not a merged pull request.`);
    return null;
  }
  return maintenanceRelease({
    branch,
    commit,
    latestRelease: latest.tag,
    latestReleaseCommit: latest.commit,
  });
}

const eventPath = z.string().parse(process.env['GITHUB_EVENT_PATH']);
const event = completedRun.parse(JSON.parse(readFileSync(eventPath, 'utf8')));
const { head_branch: branch, head_sha: commit } = event.workflow_run;
const client = createGitHubClient(z.string().min(1).parse(process.env['GITHUB_TOKEN']));
const latest = await latestPublishedRelease(client);
const plan =
  branch === 'main'
    ? await plannedReleaseFromMain(client, commit, latest)
    : await maintenanceReleaseFromBranch(client, branch, commit, latest);

if (plan === null) {
  console.log('Nothing to release.');
} else {
  if (await tagExists(client, plan.tag)) {
    throw new Error(`${plan.tag} already exists`);
  }
  console.log(`Release ${plan.tag} (${plan.kind}) from ${plan.commit}, after ${plan.baseRelease}.`);
  appendFileSync(
    z.string().parse(process.env['GITHUB_OUTPUT']),
    `tag=${plan.tag}\nkind=${plan.kind}\nbase=${plan.baseRelease}\ncommit=${plan.commit}\n`,
  );
}
