import { execFileSync } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';

const files = execFileSync('git', ['ls-files', '-z', 'tools/*.mts', 'tools/**/*.mts'], { encoding: 'utf8' })
  .split('\0').filter(Boolean);
const output = await mkdtemp(path.join(tmpdir(), 'nylzo-syntax-'));
try {
  const result = Bun.spawnSync([process.execPath, 'build', '--target=bun', '--no-bundle', '--outdir', output, ...files], { stdout: 'inherit', stderr: 'inherit' });
  process.exitCode = result.exitCode;
} finally {
  await rm(output, { recursive: true, force: true });
}
