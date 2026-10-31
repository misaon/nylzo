import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { test } from 'node:test';

const repositoryRoot = new URL('../..', import.meta.url).pathname;
const script = new URL('sync.ts', import.meta.url).pathname;
const pinnedImage = (version: string) => `services:\n  caddy:\n    image: caddy:${version}@sha256:${'a'.repeat(64)}\n`;

async function synchronizedCaddy(files: Readonly<Record<string, string>>): Promise<unknown> {
  const directory = await mkdtemp(path.join(tmpdir(), 'nylzo-dependency-sync-'));
  try {
    for (const file of [
      'tools/dependencies/assets.json',
      'deploy/hetzner-flatcar/toolchain.json',
      'mise.toml',
      'mise-tasks/deploy',
    ]) {
      await mkdir(path.join(directory, path.dirname(file)), { recursive: true });
      await copyFile(path.join(repositoryRoot, file), path.join(directory, file));
    }
    for (const [file, contents] of Object.entries(files)) {
      await mkdir(path.join(directory, path.dirname(file)), { recursive: true });
      await writeFile(path.join(directory, file), contents);
    }
    execFileSync(process.execPath, [script], { cwd: directory, stdio: 'pipe' });
    const toolchain = JSON.parse(
      await readFile(path.join(directory, 'deploy/hetzner-flatcar/toolchain.json'), 'utf8'),
    ) as Record<string, unknown>;
    return toolchain['caddy'];
  } finally {
    await rm(directory, { force: true, recursive: true });
  }
}

await test('the host toolchain takes Caddy from the module, or from Compose on a branch without it', async () => {
  const module = 'module example\n\nrequire github.com/caddyserver/caddy/v2 v2.11.9\n';
  assert.equal(
    await synchronizedCaddy({ 'deploy/caddy/go.mod': module, 'deploy/compose/compose.yaml': pinnedImage('2.11.1') }),
    '2.11.9',
  );
  assert.equal(await synchronizedCaddy({ 'deploy/compose/compose.yaml': pinnedImage('2.11.8') }), '2.11.8');
});
