import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync } from 'node:fs';
import { join, relative } from 'node:path';

import { autofixerIsClean, forbidden } from './forbidden.ts';

const app = join(import.meta.dirname, '..');
const web = join(app, '..');
const source = join(app, 'src');
const { version } = JSON.parse(readFileSync(join(app, 'node_modules/svelte/package.json'), 'utf8')) as {
  version: string;
};

const files = readdirSync(source, { recursive: true, encoding: 'utf8' })
  .filter((name) => /\.(?:svelte|ts|js)$/.test(name))
  .map((name) => join(source, name));

const problems = files.flatMap((file) => forbidden(relative(app, file), readFileSync(file, 'utf8')));

for (const file of files.filter((name) => name.endsWith('.svelte'))) {
  const path = relative(web, file);
  const output = execFileSync(
    'pnpm',
    ['--dir', web, 'exec', 'svelte-mcp', 'svelte-autofixer', path, '--svelte-version', version],
    {
      cwd: web,
      encoding: 'utf8',
    },
  );
  if (!autofixerIsClean(output)) {
    problems.push(`${path}: svelte-autofixer reported\n${output}`);
  }
}

if (problems.length > 0) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log(`checked ${files.length} files with svelte ${version}`);
