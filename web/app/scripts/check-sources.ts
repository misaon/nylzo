import { execFile } from 'node:child_process';
import { readdir, readFile } from 'node:fs/promises';
import { availableParallelism } from 'node:os';
import path from 'node:path';
import { promisify } from 'node:util';

import { sveltePolicy } from './svelte-policy.ts';

// oxlint-disable-next-line typescript/strict-void-return -- execFile has callback and ChildProcess overloads; promisify intentionally discards the child return value.
const run = promisify(execFile);
const appDirectory = path.join(import.meta.dirname, '..');
const webDirectory = path.join(appDirectory, '..');
const sourceDirectory = path.join(appDirectory, 'src');

function packageVersion(manifest: unknown): string {
  const value: unknown =
    typeof manifest === 'object' && manifest !== null ? Reflect.get(manifest, 'version') : undefined;
  if (typeof value !== 'string') {
    throw new TypeError('Svelte has no package version');
  }
  return value;
}
const svelteManifestPath = path.join(appDirectory, 'node_modules/svelte/package.json');
const svelteManifest: unknown = JSON.parse(await readFile(svelteManifestPath, 'utf8'));
const version = packageVersion(svelteManifest);
const names = await readdir(sourceDirectory, { recursive: true });
const files = names
  .filter((name) => /\.svelte(?:\.ts)?$/u.test(name))
  .map((name) => path.relative(webDirectory, path.join(sourceDirectory, name)));

async function problemsOf(file: string): Promise<string[]> {
  const source = await readFile(path.join(webDirectory, file), 'utf8');
  const problems = file.endsWith('.svelte') ? sveltePolicy(source).map((problem) => `${file}: ${problem}`) : [];
  const { stdout } = await run(
    'bun',
    ['run', '--cwd', webDirectory, '--bun', 'svelte-mcp', 'svelte-autofixer', file, '--svelte-version', version],
    { cwd: webDirectory, encoding: 'utf8' },
  );
  if (!stdout.includes('issues: []') || !stdout.includes('suggestions: []')) {
    problems.push(`${file}: svelte-autofixer reported\n${stdout}`);
  }
  return problems;
}

const queue = [...files];
const problems: string[] = [];
await Promise.all(
  Array.from({ length: availableParallelism() }, async () => {
    for (let file = queue.shift(); file !== undefined; file = queue.shift()) {
      problems.push(...(await problemsOf(file)));
    }
  }),
);

if (problems.length > 0) {
  console.error(problems.join('\n'));
  process.exitCode = 1;
} else {
  console.log(`checked ${files.length} Svelte files with svelte ${version}`);
}
