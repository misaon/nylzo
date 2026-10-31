import { appendFile, readFile } from 'node:fs/promises';
import { z } from 'zod';

import { createGitHubClient, repositoryName } from '../github/client.ts';
import { automationBot } from '../github/identity.ts';
import { createGitHubSource } from './github-source.ts';
import { decide, type Verdict } from './guard.ts';

const environment = z.object({
  GITHUB_EVENT_NAME: z.literal('pull_request_target'),
  GITHUB_EVENT_PATH: z.string().min(1),
  GITHUB_REPOSITORY: z.literal(repositoryName),
  GITHUB_STEP_SUMMARY: z.string().optional(),
  GITHUB_TOKEN: z.string().min(1),
});

const commitSha = z.string().regex(/^[a-f\d]{40}$/);
const eventPullRequest = z.object({ head: z.object({ sha: commitSha }), number: z.number().int().positive() });
const pullRequestEvent = z.object({ pull_request: eventPullRequest });

function report(verdict: Verdict): string {
  const title = verdict.passed ? 'Dependency guard passed' : 'Dependency guard failed';
  return [`### ${title}`, '', ...verdict.reasons.map((reason) => `- ${reason}`), ''].join('\n');
}

const settings = environment.parse(process.env);
const eventText = await readFile(settings.GITHUB_EVENT_PATH, 'utf8');
const event = pullRequestEvent.parse(JSON.parse(eventText));
const source = createGitHubSource(createGitHubClient(settings.GITHUB_TOKEN));
const verdict = await decide(source, automationBot(), {
  head: event.pull_request.head.sha,
  number: event.pull_request.number,
});
const summary = report(verdict);
console.log(summary);
if (settings.GITHUB_STEP_SUMMARY !== undefined) {
  await appendFile(settings.GITHUB_STEP_SUMMARY, summary);
}
if (!verdict.passed) {
  process.exitCode = 1;
}
