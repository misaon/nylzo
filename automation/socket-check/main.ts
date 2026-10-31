import { setTimeout as sleep } from 'node:timers/promises';
import { z } from 'zod';

import { createGitHubClient, repository } from '../github/client.ts';
import { socketCheckName, socketResult } from './socket-result.ts';

const waitInMilliseconds = 120_000;
const pollInMilliseconds = 5000;

const head = z
  .string()
  .regex(/^[\da-f]{40}$/)
  .parse(process.env['SOCKET_HEAD_SHA']);
const client = createGitHubClient(z.string().min(1).parse(process.env['GITHUB_TOKEN']));
const deadline = Date.now() + waitInMilliseconds;

while (Date.now() < deadline) {
  const checks = await client.paginate(client.rest.checks.listForRef, {
    ...repository,
    check_name: socketCheckName,
    filter: 'all',
    ref: head,
  });
  const result = socketResult(checks, head);
  if (result === 'success') {
    console.log('The exact PR head passed the required Socket App check.');
    process.exit(0);
  }
  if (result === 'failure') {
    throw new Error('Socket rejected this exact PR head; inspect its findings before merging.');
  }
  await sleep(pollInMilliseconds);
}
throw new Error('Socket has not completed; no merge is authorized.');
