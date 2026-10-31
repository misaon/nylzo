import assert from 'node:assert/strict';
import { test } from 'node:test';

import { type CheckRun, socketResult } from './socket-result.ts';

const head = 'a'.repeat(40);
const passed: CheckRun = {
  app: { id: 156_372 },
  conclusion: 'success',
  head_sha: head,
  id: 1,
  name: 'Socket Security: Pull Request Alerts',
  status: 'completed',
};

test('only the newest matching Socket App check on the exact PR head can pass', () => {
  assert.equal(socketResult([passed], head), 'success');
  for (const changed of [
    [],
    [{ ...passed, app: { id: 15_368 } }],
    [{ ...passed, head_sha: 'b'.repeat(40) }],
    [{ ...passed, name: 'Socket Security: Project Report' }],
    [{ ...passed, conclusion: null, status: 'in_progress' }],
  ]) {
    assert.equal(socketResult(changed, head), 'pending');
  }
  for (const conclusion of ['failure', 'neutral', 'skipped', 'cancelled', 'timed_out', 'action_required']) {
    assert.equal(socketResult([passed, { ...passed, conclusion, id: 2 }], head), 'failure');
  }
  assert.equal(
    socketResult([passed, { ...passed, conclusion: null, id: 2, status: 'in_progress' }], head),
    'pending',
    'An earlier success cannot bypass an unfinished replacement',
  );
});
