import { spawnSync } from 'node:child_process';
import path from 'node:path';

import { verifyInstallPolicy } from './policy.ts';

verifyInstallPolicy();

const webDirectory = path.resolve(import.meta.dirname, '../../web');

const install = spawnSync('bun', ['install', '--cwd', webDirectory, '--frozen-lockfile', '--ignore-scripts'], {
  stdio: 'inherit',
});
process.exitCode = install.status ?? 1;
