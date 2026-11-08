import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { parseArgs } from 'node:util';

import { z } from 'zod';

import { createGitHubClient, repository, unlessMissing } from '../github/client.ts';
import type { GitHubClient } from '../github/client.ts';
import { maintenanceBranch, parseVersion } from '../versions/semantic-version.ts';
import { globalSettings } from './global-settings.ts';

const inventory = z.object({
  format: z.literal(1),
  packages: z.array(z.string().regex(/^[\w@./+-]+$/u)),
});

const assets = z.record(z.string(), z.object({ version: z.string() }));

async function existingBranch(client: GitHubClient, branch: string): Promise<string | null> {
  return (await unlessMissing(client.rest.repos.getBranch({ ...repository, branch }))) === undefined ? null : branch;
}

function affectedPackages(path: string | undefined): string[] {
  return path === undefined || !existsSync(path)
    ? []
    : inventory.parse(JSON.parse(readFileSync(path, 'utf8'))).packages;
}

const assetsFile = new URL('../../tools/dependencies/assets.json', import.meta.url);

function pinnedMiseVersion(): string {
  const pins = assets.parse(JSON.parse(readFileSync(assetsFile, 'utf8')));
  const mise = pins['mise-amd64'];
  if (mise === undefined) {
    throw new Error('tools/dependencies/assets.json pins no mise-amd64 release');
  }
  return mise.version.replace(/^v/u, '');
}

const { values } = parseArgs({
  options: { affected: { type: 'string' }, output: { type: 'string' } },
  strict: true,
});
if (values.output === undefined) {
  throw new Error('Usage: main.ts --output <file> [--affected <inventory affected.json>]');
}

const client = createGitHubClient(z.string().min(1).parse(process.env['GITHUB_TOKEN']));
const latest = await client.rest.repos.getLatestRelease(repository);
const maintenance = await existingBranch(client, maintenanceBranch(parseVersion(latest.data.tag_name)));
const fastPath = affectedPackages(values.affected);
const manifestText = readFileSync(new URL('../package.json', import.meta.url), 'utf8');
const runtime = z.object({ packageManager: z.string().regex(/^bun@\d+\.\d+\.\d+$/u) }).parse(JSON.parse(manifestText));
const settings = globalSettings({
  affectedPackages: fastPath,
  maintenanceBranch: maintenance,
  miseVersion: pinnedMiseVersion(),
  bunVersion: runtime.packageManager.slice(4),
});
writeFileSync(values.output, `${JSON.stringify(settings, undefined, 2)}\n`);
console.log(
  `Renovate processes main${maintenance === null ? '' : ` and ${maintenance}`}; ${fastPath.length} affected dependencies take the fast path.`,
);
