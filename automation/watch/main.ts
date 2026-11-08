import { z } from 'zod';

import { createGitHubClient, repository } from '../github/client.ts';
import type { GitHubClient } from '../github/client.ts';
import { dueWorkflows } from './due.ts';

const workflowRun = z.object({
  created_at: z.string(),
  display_title: z.string(),
  id: z.number(),
  status: z.string().nullable(),
});
const runs = z.object({ workflow_runs: z.array(workflowRun) });
const dispatchedRun = z.object({ html_url: z.url() });

type WorkflowRun = z.infer<typeof workflowRun>;

async function recentRuns(client: GitHubClient, workflow: string, event?: string): Promise<WorkflowRun[]> {
  const response = await client.request('GET /repos/{owner}/{repo}/actions/workflows/{workflow_id}/runs', {
    ...repository,
    workflow_id: workflow,
    per_page: 20,
    ...(event === undefined ? {} : { event }),
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
  const response = await client.request('POST /repos/{owner}/{repo}/actions/workflows/{workflow_id}/dispatches', {
    ...repository,
    workflow_id: workflow,
    ref: 'main',
    inputs,
    // API version 2022-11-28 of this endpoint is deprecated; 2026-03-10 answers with the started run.
    headers: { 'x-github-api-version': '2026-03-10' },
  });
  const run = dispatchedRun.parse(response.data);
  console.log(
    `Started ${workflow}${Object.keys(inputs).length === 0 ? '' : ` with ${JSON.stringify(inputs)}`}: ${run.html_url}`,
  );
}

const client = createGitHubClient(z.string().min(1).parse(process.env['GITHUB_TOKEN']));
const currentRun = Number(z.string().regex(/^\d+$/u).parse(process.env['GITHUB_RUN_ID']));
const [renovateRuns, releaseTagRuns, watchRuns] = await Promise.all([
  recentRuns(client, 'renovate.yml', 'workflow_dispatch'),
  recentRuns(client, 'release-tag.yml', 'workflow_dispatch'),
  recentRuns(client, 'watch.yml'),
]);
const due = dueWorkflows(Date.now(), {
  renovate: newest(renovateRuns),
  dependencyWindow: newest(releaseTagRuns.filter((run) => run.display_title.includes('dependency-window'))),
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
