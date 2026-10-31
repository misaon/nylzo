import { spawnSync } from 'node:child_process';
import path from 'node:path';

import { verifyDeepmergePatch } from './deepmerge-patch.ts';

const webDirectory = path.resolve(import.meta.dirname, '../../web');

const install = spawnSync(
  'pnpm',
  ['--dir', webDirectory, 'install', '--frozen-lockfile', '--config.confirmModulesPurge=false'],
  { stdio: 'inherit' },
);
process.exitCode = install.status ?? 1;
if (install.status === 0) {
  verifyDeepmergePatch();
}
