import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';

export const localWorkspaces: Readonly<Record<string, string>> = {
  '@nylzo/core': 'core',
  '@nylzo/web': 'app',
  '@nylzo/e2e': 'e2e',
};

export function verifyInstallPolicy(root = path.resolve(import.meta.dirname, '../..')): void {
  const mise = Bun.TOML.parse(readFileSync(path.join(root, 'mise.toml'), 'utf8'));
  const tools: unknown = Reflect.get(mise, 'tools');
  assert.ok(typeof tools === 'object' && tools !== null, 'Missing toolchain pins');
  const bun: unknown = Reflect.get(tools, 'bun');
  assert.ok(typeof bun === 'string' && /^\d+\.\d+\.\d+$/u.test(bun), 'Bun must have an exact version pin');
  for (const [name, directory] of Object.entries(localWorkspaces)) {
    const manifest: unknown = JSON.parse(readFileSync(path.join(root, 'web', directory, 'package.json'), 'utf8'));
    assert.ok(typeof manifest === 'object' && manifest !== null, 'Invalid local workspace');
    assert.equal(Reflect.get(manifest, 'name'), name, 'Local workspace identity must match its directory');
    assert.equal(Reflect.get(manifest, 'license'), 'MIT', 'Local workspace licensing must match LICENSE');
  }
  for (const workspace of ['web', 'automation']) {
    const directory = path.join(root, workspace);
    const manifest: unknown = JSON.parse(readFileSync(path.join(directory, 'package.json'), 'utf8'));
    assert.ok(typeof manifest === 'object' && manifest !== null, 'Invalid manifest');
    assert.equal(Reflect.get(manifest, 'packageManager'), `bun@${bun}`, 'Package manager must match mise');
    const engines: unknown = Reflect.get(manifest, 'engines');
    assert.ok(
      typeof engines === 'object' && engines !== null && Reflect.get(engines, 'bun') === bun,
      'Bun engine must match mise',
    );
    assert.deepEqual(Reflect.get(manifest, 'trustedDependencies'), [], 'Dependency lifecycle scripts must be disabled');
    const configuration = Bun.TOML.parse(readFileSync(path.join(directory, 'bunfig.toml'), 'utf8'));
    const run: unknown = Reflect.get(configuration, 'run');
    assert.ok(
      typeof run === 'object' && run !== null && Reflect.get(run, 'bun') === true,
      'Package binaries must run under Bun',
    );
    const install: unknown = Reflect.get(configuration, 'install');
    assert.ok(typeof install === 'object' && install !== null, 'Missing install policy');
    const minimumAge: unknown = Reflect.get(install, 'minimumReleaseAge');
    assert.ok(typeof minimumAge === 'number' && minimumAge >= 86_400, 'Packages must age at least 24 hours');
    assert.deepEqual(Reflect.get(install, 'minimumReleaseAgeExcludes') ?? [], [], 'No package may bypass quarantine');
    assert.equal(Reflect.get(install, 'linker'), 'isolated', 'Prevent undeclared dependencies');
    const lock: unknown = Bun.JSON5.parse(readFileSync(path.join(directory, 'bun.lock'), 'utf8'));
    assert.ok(typeof lock === 'object' && lock !== null, 'Missing lockfile');
    assert.equal(Reflect.get(lock, 'configVersion'), 1, 'Use isolated Bun lockfiles');
    assert.deepEqual(Reflect.get(lock, 'trustedDependencies') ?? [], [], 'Lockfile cannot trust lifecycle scripts');
  }
}

if (import.meta.main) {
  verifyInstallPolicy();
}
