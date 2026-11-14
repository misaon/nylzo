import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import { upgradeProblem } from '../app-update.ts';
import { bunLock } from './lockfiles.ts';
import { packageManifest } from './manifests.ts';

const locked = readFileSync(new URL('../../../web/bun.lock', import.meta.url), 'utf8');
const review = bunLock(['@nylzo/core@workspace:core', '@nylzo/web@workspace:app', '@nylzo/e2e@workspace:e2e']);

test('Bun dependency review accepts the real lock and rejects policy, source and integrity tampering', () => {
  assert.deepEqual(review('web/bun.lock', locked, locked).findings, []);
  const lock = Bun.JSON5.parse(locked) as Record<string, unknown>;
  const packages = lock['packages'] as Record<string, unknown[]>;
  const entry = packages['deepmerge'];
  assert.ok(entry);
  for (const mutated of [
    { ...lock, trustedDependencies: ['deepmerge'] },
    { ...lock, patchedDependencies: {} },
    { ...lock, configVersion: 0 },
    { ...lock, packages: { ...packages, deepmerge: [entry[0], 'https://evil.test/package.tgz', entry[2], entry[3]] } },
    { ...lock, packages: { ...packages, deepmerge: [entry[0], '', entry[2], ''] } },
    { ...lock, packages: { ...packages, deepmerge: ['deepmerge@git+https://evil.test/repo', '', {}, entry[3]] } },
    { ...lock, packages: { ...packages, evil: ['evil@workspace:../../outside'] } },
    { ...lock, packages: {} },
  ]) {
    assert.ok(review('web/bun.lock', locked, JSON.stringify(mutated)).findings.length > 0);
  }
  assert.ok(review('web/bun.lock', locked, 'not JSON').findings.length > 0);
});

test('Bun lockfile overrides may advance a version but not change their list or form', () => {
  const lock = Bun.JSON5.parse(locked) as Record<string, unknown>;
  const overrides = lock['overrides'] as Record<string, string>;
  const withOverrides = (changed: Record<string, string>): string =>
    JSON.stringify({ ...lock, overrides: { ...overrides, ...changed } });
  const [major = 0, minor = 0, patchLevel = 0] = (overrides['minimatch'] ?? '').split('.').map(Number);
  const newerPatch = `${major}.${minor}.${patchLevel + 1}`;
  const patch = review('web/bun.lock', locked, withOverrides({ minimatch: newerPatch }));
  assert.deepEqual(patch.findings, []);
  assert.deepEqual(patch.updates.map(upgradeProblem), [undefined]);
  const nextMajor = review('web/bun.lock', locked, withOverrides({ minimatch: `${major + 1}.0.0` }));
  assert.ok(nextMajor.updates.some((update) => upgradeProblem(update) !== undefined));
  for (const changed of [
    { attacker: '1.0.0' },
    { minimatch: `^${newerPatch}` },
    { attacker: '1.0.0', minimatch: newerPatch },
  ]) {
    assert.ok(review('web/bun.lock', locked, withOverrides(changed)).findings.length > 0);
  }
  const { minimatch: _removed, ...remaining } = overrides;
  assert.ok(review('web/bun.lock', locked, JSON.stringify({ ...lock, overrides: remaining })).findings.length > 0);
});

test('Bun engine pins and native compiler aliases may advance without changing their identity', () => {
  const before = JSON.stringify({
    name: 'example',
    packageManager: 'bun@1.4.3',
    engines: { bun: '1.4.3' },
    devDependencies: { '@typescript/native': 'npm:typescript@7.0.1' },
  });
  const after = before.replaceAll('1.4.3', '1.4.4').replace('7.0.1', '7.0.2');
  const result = packageManifest('web/package.json', before, after);
  assert.deepEqual(result.findings, []);
  assert.ok(result.updates.some((update) => update.after === '7.0.2' && update.before === '7.0.1'));
  assert.ok(
    packageManifest('web/package.json', before, after.replace('npm:typescript', 'npm:foreign')).findings.length > 0,
  );
  assert.ok(
    packageManifest('web/package.json', before, after.replace('"bun":"1.4.4"', '"bun":"1.4.5"')).findings.length > 0,
  );
});
