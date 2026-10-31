import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

const workflows = new URL('../../.github/workflows/', import.meta.url);

function jobBlock(workflow: string, job: string): string {
  const header = `\n  ${job}:\n`;
  const start = workflow.indexOf(header);
  assert.notEqual(start, -1, `The workflow has no ${job} job`);
  const body = workflow.slice(start + header.length);
  const end = body.search(/^ {2}[a-z]/m);
  return end === -1 ? body : body.slice(0, end);
}

test('CI cannot execute frontend builds or tooling before the source dependency gate', async () => {
  const ci = await readFile(new URL('ci.yml', workflows), 'utf8');
  for (const job of ['web', 'e2e', 'build', 'repository', 'deployment', 'dev-setup', 'compose']) {
    assert.match(jobBlock(ci, job), /^ {4}needs: \[changes, sca\]$/m, `${job} must wait for SCA`);
  }
  const gate = jobBlock(ci, 'sca');
  assert.match(gate, /run: mise run lint:dependencies$/m);
  assert.doesNotMatch(gate, /pnpm (--dir automation|-C automation) install/, 'The gate must not run unvetted tooling');
  const mise = await readFile(new URL('../../mise.toml', import.meta.url), 'utf8');
  const task = (name: string) => {
    const start = mise.indexOf(`[tasks."${name}"]\n`);
    assert.notEqual(start, -1, `mise.toml has no ${name} task`);
    return mise.slice(start).split('\n\n', 1)[0] ?? '';
  };
  assert.match(task('lint:dependencies'), /^depends = \["install:web"\]$/m, 'The gate must not run unvetted tooling');
  assert.match(
    task('lint:dependencies'),
    /^run = "node automation\/source-dependency-gate\/main\.ts \.sca-reports --sbom"$/m,
  );
  assert.match(task('install:web'), /^run = "node automation\/web-install\/main\.ts"$/m);
  const release = await readFile(new URL('release.yml', workflows), 'utf8');
  const scan = release.indexOf(
    'run: node automation/source-dependency-gate/main.ts "$RUNNER_TEMP/release-evidence" --sbom',
  );
  assert.notEqual(scan, -1, 'The release must scan locked packages');
  assert.ok(
    scan < release.indexOf('pnpm --dir web --filter @nylzo/web build'),
    'The release must scan before building',
  );
});
