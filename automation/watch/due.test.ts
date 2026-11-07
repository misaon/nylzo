import assert from 'node:assert/strict';
import { test } from 'node:test';

import { dueWorkflows } from './due.ts';

const wednesday = Date.parse('2026-11-11T14:00:00Z');
const hour = 60 * 60 * 1000;

test('Renovate is due once six hours have passed since its last start', () => {
  assert.equal(
    dueWorkflows(wednesday, { renovate: wednesday - 5 * hour, dependencyWindow: wednesday }).renovate,
    false,
  );
  assert.equal(dueWorkflows(wednesday, { renovate: wednesday - 6 * hour, dependencyWindow: wednesday }).renovate, true);
  assert.equal(dueWorkflows(wednesday, { renovate: null, dependencyWindow: wednesday }).renovate, true);
});

test('a dependency release window opens on Monday, Wednesday and Friday after 13:47 UTC, once', () => {
  const opened = Date.parse('2026-11-11T13:50:00Z');
  assert.equal(dueWorkflows(wednesday, { renovate: wednesday, dependencyWindow: null }).dependencyWindow, true);
  assert.equal(dueWorkflows(wednesday, { renovate: wednesday, dependencyWindow: opened }).dependencyWindow, false);
  assert.equal(
    dueWorkflows(Date.parse('2026-11-11T13:00:00Z'), { renovate: wednesday, dependencyWindow: null }).dependencyWindow,
    false,
  );
  assert.equal(
    dueWorkflows(Date.parse('2026-11-12T14:00:00Z'), { renovate: wednesday, dependencyWindow: null }).dependencyWindow,
    false,
  );
  assert.equal(
    dueWorkflows(Date.parse('2026-11-13T23:00:00Z'), { renovate: wednesday, dependencyWindow: opened })
      .dependencyWindow,
    true,
  );
});
