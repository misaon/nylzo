import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { test } from 'node:test';

import { objectAt, textAt } from './json-values.ts';
import { npmRuntimeEvidence } from './runtime-dependencies.ts';

const repositoryRoot = path.resolve(import.meta.dirname, '../..');

const lock = `{
  "lockfileVersion": 1,
  "workspaces": {
    "": { "name": "root", "devDependencies": { "tool": "1.0.0" } },
    "app": {
      "name": "@example/app",
      "dependencies": { "@example/core": "workspace:*", "ui": "2.0.0" },
      "devDependencies": { "bundler": "3.0.0" },
    },
    "core": { "name": "@example/core", "dependencies": { "cipher": "1.0.0" } },
  },
  "packages": {
    "@example/app": ["@example/app@workspace:app"],
    "@example/core": ["@example/core@workspace:core"],
    "bundler": ["bundler@3.0.0", "", { "dependencies": { "matcher": "4.0.7" } }, "sha512-a"],
    "cipher": ["cipher@1.0.0", "", {}, "sha512-b"],
    "matcher": ["matcher@4.0.7", "", {}, "sha512-c"],
    "tool": ["tool@1.0.0", "", { "dependencies": { "matcher": "4.0.7" } }, "sha512-d"],
    "ui": ["ui@2.0.0", "", { "dependencies": { "matcher": "^5" }, "optionalDependencies": { "native": "1" } }, "sha512-e"],
    "ui/matcher": ["matcher@5.0.0", "", {}, "sha512-f"],
  },
}`;

test('the runtime closure follows workspaces and nested versions but leaves development tools out', () => {
  const evidence = npmRuntimeEvidence(lock);
  assert.deepEqual(evidence.runtime, ['@example/core@workspace:core', 'cipher@1.0.0', 'matcher@5.0.0', 'ui@2.0.0']);
  assert.ok(evidence.locked.includes('matcher@4.0.7'));
  const hoisted = npmRuntimeEvidence(lock.replace('"ui/matcher": ["matcher@5.0.0", "", {}, "sha512-f"],', ''));
  assert.ok(
    hoisted.runtime.includes('matcher@4.0.7'),
    'A runtime package that falls back to the hoisted copy reaches it',
  );
});

test('a runtime dependency missing from the lockfile fails instead of shrinking the closure', () => {
  assert.throws(
    () => npmRuntimeEvidence(lock.replace('"cipher": ["cipher@1.0.0", "", {}, "sha512-b"],', '')),
    /Unresolved runtime dependency cipher/u,
  );
});

test('the runtime closure of each repository lockfile holds every runtime dependency its manifests declare', () => {
  const workspaces = { 'automation/bun.lock': ['automation'], 'web/bun.lock': ['web/app', 'web/core'] };
  for (const [lockfile, directories] of Object.entries(workspaces)) {
    const evidence = npmRuntimeEvidence(readFileSync(path.join(repositoryRoot, lockfile), 'utf8'));
    for (const directory of directories) {
      const manifestText = readFileSync(path.join(repositoryRoot, directory, 'package.json'), 'utf8');
      const manifest = objectAt(JSON.parse(manifestText), directory);
      const dependencies = manifest['dependencies'] === undefined ? {} : objectAt(manifest['dependencies'], directory);
      for (const [name, value] of Object.entries(dependencies)) {
        const version = textAt(value, `${directory} ${name}`);
        const expected = version.startsWith('workspace:') ? `${name}@workspace:` : `${name}@${version}`;
        assert.ok(
          evidence.runtime.some((id) => id.startsWith(expected)),
          `${lockfile} runtime closure lacks ${name} from ${directory}`,
        );
      }
    }
  }
});
