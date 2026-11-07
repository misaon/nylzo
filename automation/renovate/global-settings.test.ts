import assert from 'node:assert/strict';
import { test } from 'node:test';

import { globalSettings } from './global-settings.ts';

test('the maintenance branch joins main and the fast path enables affected packages', () => {
  const settings = globalSettings({
    affectedPackages: ['goreleaser/goreleaser'],
    maintenanceBranch: 'release/v0.2',
    bunVersion: '1.4.4',
    miseVersion: '2026.10.19',
  });
  assert.deepEqual(settings['force'], { baseBranchPatterns: ['main', 'release/v0.2'] });
  assert.deepEqual(settings['packageRules'], [
    {
      description: 'The vulnerability inventory found these dependencies affected; propose their fix at once.',
      matchPackageNames: ['goreleaser/goreleaser'],
      enabled: true,
      minimumReleaseAge: null,
      schedule: [],
      addLabels: ['security'],
    },
  ]);
});

test('without a maintenance branch Renovate processes only main', () => {
  const settings = globalSettings({
    affectedPackages: [],
    maintenanceBranch: null,
    bunVersion: '1.4.4',
    miseVersion: '2026.10.19',
  });
  assert.deepEqual(settings['force'], { baseBranchPatterns: ['main'] });
  assert.deepEqual(settings['packageRules'], []);
});
