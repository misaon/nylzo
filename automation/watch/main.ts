import { z } from 'zod';

import { createGitHubClient, repository } from '../github/client.ts';
import type { GitHubClient } from '../github/client.ts';
import { dueWorkflows } from './due.ts';

const workflowRun = z.object({
  created_at: z.string(),
  display_title: z.string(),
  event: z.string(),
  id: z.number(),
  status: z.string().nullable(),
});
const runs = z.object({ workflow_runs: z.array(workflowRun) });

type WorkflowRun = z.infer<typeof workflowRun>;

async function recentRuns(client: GitHubClient, workflow: string): Promise<WorkflowRun[]> {
  const response = await client.request('GET /repos/{owner}/{repo}/actions/workflows/{workflow_id}/runs', {
    ...repository,
    workflow_id: workflow,
    per_page: 50,
  });
  return runs.parse(response.data).workflow_runs;
}

function newest(candidates: readonly WorkflowRun[]): number | null {
  const times = candidates.map((run) => Date.parse(run.created_at));
  return times.length === 0 ? null : Math.max(...times);
}

async function dispatch(
  client: GitHubClient,
  workflow: string,
  inputs: Readonly<Record<string, string>> = {},
): Promise<void> {
  await client.request('POST /repos/{owner}/{repo}/actions/workflows/{workflow_id}/dispatches', {
    ...repository,
    workflow_id: workflow,
    ref: 'main',
    inputs,
  });
  console.log(`Started ${workflow}${Object.keys(inputs).length === 0 ? '' : ` with ${JSON.stringify(inputs)}`}.`);
}

const client = createGitHubClient(z.string().min(1).parse(process.env['GITHUB_TOKEN']));
const currentRun = Number(z.string().regex(/^\d+$/u).parse(process.env['GITHUB_RUN_ID']));
const [renovateRuns, releaseTagRuns, watchRuns] = await Promise.all([
  recentRuns(client, 'renovate.yml'),
  recentRuns(client, 'release-tag.yml'),
  recentRuns(client, 'watch.yml'),
]);
const due = dueWorkflows(Date.now(), {
  renovate: newest(renovateRuns.filter((run) => run.event === 'schedule' || run.event === 'workflow_dispatch')),
  dependencyWindow: newest(
    releaseTagRuns.filter((run) => run.event === 'schedule' || run.display_title.includes('dependency-window')),
  ),
});
if (due.renovate) {
  await dispatch(client, 'renovate.yml');
}
if (due.dependencyWindow) {
  await dispatch(client, 'release-tag.yml', { task: 'dependency-window' });
}
const waiting = watchRuns.some(
  (run) => run.id !== currentRun && ['queued', 'waiting', 'pending', 'requested'].includes(run.status ?? ''),
);
if (process.env['WATCH_CHAIN'] === 'true' && !waiting) {
  await dispatch(client, 'watch.yml');
}
