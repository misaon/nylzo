import assert from 'node:assert/strict';
import { test } from 'node:test';

import { assessedAdvisories } from './audit.ts';
import { parseScaPolicy } from './sca-policy.ts';

const policy = parseScaPolicy({
  allowedLicenses: ['MIT'],
  fixed: [],
  notAffected: [
    {
      ecosystem: 'npm',
      id: 'GHSA-fc5q-39xc-vjpf',
      impact: 'Only build tools run it.',
      justification: 'vulnerable_code_cannot_be_controlled_by_adversary',
      lockfile: 'web/bun.lock',
      package: 'picomatch',
      reviewUntil: '2027-01-15T00:00:00Z',
      version: '4.0.7',
    },
  ],
});

test('bun audit skips an advisory only for the lockfile its assessment names and only until the review date', () => {
  assert.deepEqual(assessedAdvisories(policy, 'web/bun.lock', new Date('2026-11-18T00:00:00Z')), [
    'GHSA-fc5q-39xc-vjpf',
  ]);
  assert.deepEqual(assessedAdvisories(policy, 'automation/bun.lock', new Date('2026-11-18T00:00:00Z')), []);
  assert.deepEqual(assessedAdvisories(policy, 'web/bun.lock', new Date('2027-01-15T00:00:00Z')), []);
});
