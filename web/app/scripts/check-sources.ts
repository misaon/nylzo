import { execFile } from 'node:child_process';
import { readdir, readFile } from 'node:fs/promises';
import { availableParallelism } from 'node:os';
import path from 'node:path';
import { promisify } from 'node:util';

const run = promisify(execFile);
const appDirectory = path.join(import.meta.dirname, '..');
const webDirectory = path.join(appDirectory, '..');
const sourceDirectory = path.join(appDirectory, 'src');
// ESLint has no rule for this binding, which writes markup like innerHTML.
const MARKUP_BINDING = /\bbind:innerHTML\b/;

const { version } = JSON.parse(await readFile(path.join(appDirectory, 'node_modules/svelte/package.json'), 'utf8')) as {
  version: string;
};
const names = await readdir(sourceDirectory, { recursive: true });
const files = names
  .filter((name) => /\.svelte(?:\.ts)?$/.test(name))
  .map((name) => path.relative(webDirectory, path.join(sourceDirectory, name)));

async function problemsOf(file: string): Promise<string[]> {
  const problems = MARKUP_BINDING.test(await readFile(path.join(webDirectory, file), 'utf8'))
    ? [`${file}: bind:innerHTML parses markup`]
    : [];
  const { stdout } = await run(
    'pnpm',
    ['--dir', webDirectory, 'exec', 'svelte-mcp', 'svelte-autofixer', file, '--svelte-version', version],
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
