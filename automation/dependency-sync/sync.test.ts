import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { test } from 'node:test';

const repositoryRoot = new URL('../..', import.meta.url).pathname;
const script = new URL('sync.ts', import.meta.url).pathname;
const toolchainPath = 'deploy/hetzner-flatcar/toolchain.json';
const deployTaskPath = 'mise-tasks/deploy';
const pinnedImage = (version: string) => `services:\n  caddy:\n    image: caddy:${version}@sha256:${'a'.repeat(64)}\n`;

async function synchronized(
  files: Readonly<Record<string, string>>,
): Promise<{ toolchain: Record<string, unknown>; deployTask: string | undefined }> {
  const directory = await mkdtemp(path.join(tmpdir(), 'nylzo-dependency-sync-'));
  try {
    for (const file of ['tools/dependencies/assets.json', toolchainPath]) {
      await mkdir(path.join(directory, path.dirname(file)), { recursive: true });
      await copyFile(path.join(repositoryRoot, file), path.join(directory, file));
    }
    for (const [file, contents] of Object.entries(files)) {
      await mkdir(path.join(directory, path.dirname(file)), { recursive: true });
      await writeFile(path.join(directory, file), contents);
    }
    execFileSync(process.execPath, [script], { cwd: directory, stdio: 'pipe' });
    const read = (file: string) => readFile(path.join(directory, file), 'utf8');
    return {
      toolchain: JSON.parse(await read(toolchainPath)) as Record<string, unknown>,
      deployTask: files[deployTaskPath] === undefined ? undefined : await read(deployTaskPath),
    };
  } finally {
    await rm(directory, { force: true, recursive: true });
  }
}

await test('the host toolchain takes Caddy from the module, or from Compose on a branch without it', async () => {
  const module = 'module example\n\nrequire github.com/caddyserver/caddy/v2 v2.11.9\n';
  const fromModule = await synchronized({
    'deploy/caddy/go.mod': module,
    'deploy/compose/compose.yaml': pinnedImage('2.11.1'),
  });
  assert.equal(fromModule.toolchain['caddy'], '2.11.9');
  const fromCompose = await synchronized({ 'deploy/compose/compose.yaml': pinnedImage('2.11.8') });
  assert.equal(fromCompose.toolchain['caddy'], '2.11.8');
});

await test('a maintenance branch keeps its OpenTofu pins in step with mise.toml', async () => {
  const { flatcar, ...tools } = JSON.parse(await readFile(path.join(repositoryRoot, toolchainPath), 'utf8')) as Record<
    string,
    unknown
  >;
  const result = await synchronized({
    [toolchainPath]: JSON.stringify({ flatcar, opentofu: '1.13.2', ...tools }),
    'mise.toml': '[tools]\nopentofu = "1.13.9"\n',
    [deployTaskPath]: 'exec mise exec opentofu@1.13.2 -- bun tools/deploy/hetzner.mts "$@"\n',
    'deploy/compose/compose.yaml': pinnedImage('2.11.8'),
  });
  assert.equal(result.toolchain['opentofu'], '1.13.9');
  assert.equal(result.deployTask, 'exec mise exec opentofu@1.13.9 -- bun tools/deploy/hetzner.mts "$@"\n');
});
