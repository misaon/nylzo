import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { parseArgs } from 'node:util';

import { parseScaPolicy } from './sca-policy.ts';
import type { ScaPolicy } from './sca-policy.ts';

const repositoryRoot = path.resolve(import.meta.dirname, '../..');
const workspaces = ['web', 'automation'];

export function assessedAdvisories(policy: ScaPolicy, lockfile: string, now: Date): string[] {
  return policy.notAffected
    .filter((assessment) => assessment.lockfile === lockfile && Date.parse(assessment.reviewUntil) > now.getTime())
    .map((assessment) => assessment.id);
}

if (import.meta.main) {
  const { positionals } = parseArgs({ allowPositionals: true });
  const [workspace] = positionals;
  assert.ok(workspace !== undefined && workspaces.includes(workspace), `Audit one of: ${workspaces.join(', ')}`);
  const policyText = readFileSync(path.join(repositoryRoot, 'security/sca-policy.json'), 'utf8');
  const policy = parseScaPolicy(JSON.parse(policyText));
  const ignored = assessedAdvisories(policy, `${workspace}/bun.lock`, new Date());
  const audit = spawnSync(
    process.execPath,
    ['audit', `--cwd=${workspace}`, '--audit-level=moderate', ...ignored.map((id) => `--ignore=${id}`)],
    { cwd: repositoryRoot, stdio: 'inherit' },
  );
  process.exitCode = audit.status ?? 1;
}
