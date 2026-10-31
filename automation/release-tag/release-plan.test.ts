import assert from 'node:assert/strict';
import { test } from 'node:test';

import { maintenanceRelease, plannedRelease } from './release-plan.ts';

const commit = 'a'.repeat(40);
const releaseCommit = 'b'.repeat(40);

test('a merged fix on the current maintenance branch becomes the next patch', () => {
  assert.deepEqual(
    maintenanceRelease({
      branch: 'release/v0.2',
      commit,
      latestRelease: 'v0.2.3',
      latestReleaseCommit: releaseCommit,
    }),
    { baseRelease: 'v0.2.3', commit, kind: 'security', tag: 'v0.2.4' },
  );
});

test('the maintenance branch at the published commit has nothing to release', () => {
  assert.equal(
    maintenanceRelease({
      branch: 'release/v0.2',
      commit: releaseCommit,
      latestRelease: 'v0.2.3',
      latestReleaseCommit: releaseCommit,
    }),
    null,
  );
});

test('an older maintenance line cannot publish a patch after a newer release', () => {
  assert.throws(() =>
    maintenanceRelease({
      branch: 'release/v0.2',
      commit,
      latestRelease: 'v0.3.0',
      latestReleaseCommit: releaseCommit,
    }),
  );
});

test('a merged release PR publishes the manifest version above the latest release', () => {
  assert.deepEqual(plannedRelease({ commit, latestRelease: 'v0.2.5', manifestVersion: '0.3.0' }), {
    baseRelease: 'v0.2.5',
    commit,
    kind: 'planned',
    tag: 'v0.3.0',
  });
});

test('a planned release never reuses or skips into the patch line', () => {
  assert.throws(() => plannedRelease({ commit, latestRelease: 'v0.3.0', manifestVersion: '0.3.0' }));
  assert.throws(() => plannedRelease({ commit, latestRelease: 'v0.2.5', manifestVersion: '0.2.6' }));
  assert.throws(() => plannedRelease({ commit, latestRelease: 'v0.2.5', manifestVersion: '0.3.1' }));
});
