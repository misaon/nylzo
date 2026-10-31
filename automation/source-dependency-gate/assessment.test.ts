import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

import { deepmergePatch, type PatchAssessment } from '../web-install/deepmerge-patch.ts';
import { assessSourceDependencies, type VexStatement } from './assessment.ts';
import { releasePlatforms } from './go-imports.ts';
import { parseDependencyReport } from './osv-report.ts';
import { parseScaPolicy, type ScaPolicy } from './sca-policy.ts';

interface PackageEntry {
  licenses: string[];
  package: { ecosystem: string; name: string; version: string };
  vulnerabilities?: VulnerabilityEntry[];
}

interface VulnerabilityEntry {
  aliases?: string[];
  id: string;
}

const policyText = await readFile(new URL('../../security/sca-policy.json', import.meta.url), 'utf8');
const repositoryPolicy = parseScaPolicy(JSON.parse(policyText));

function fixture() {
  const cryptoVulnerability: VulnerabilityEntry = { id: 'GO-2026-5932' };
  const crypto: PackageEntry = {
    licenses: ['BSD-3-Clause'],
    package: { ecosystem: 'Go', name: 'golang.org/x/crypto', version: '0.58.0' },
    vulnerabilities: [cryptoVulnerability],
  };
  const svelte: PackageEntry = {
    licenses: ['MIT'],
    package: { ecosystem: 'npm', name: 'svelte', version: '5.57.2' },
  };
  const zod: PackageEntry = { licenses: ['MIT'], package: { ecosystem: 'npm', name: 'zod', version: '4.6.7' } };
  const web = { packages: [svelte], source: { path: '/project/web/pnpm-lock.yaml' } };
  const automation = { packages: [zod], source: { path: '/project/automation/pnpm-lock.yaml' } };
  const report = {
    results: [{ packages: [crypto], source: { path: '/project/go.mod' } }, web, automation],
  };
  const cryptoAssessment = {
    ecosystem: 'Go',
    forbiddenImport: 'golang.org/x/crypto/openpgp',
    id: 'GO-2026-5932',
    impact: 'OpenPGP is absent.',
    justification: 'vulnerable_code_not_present',
    package: 'golang.org/x/crypto',
    reviewUntil: '2027-01-25T00:00:00Z',
    version: '0.58.0',
  };
  const policy = {
    allowedLicenses: ['BSD-3-Clause', 'MIT', 'Apache-2.0'],
    notAffected: [cryptoAssessment],
    toolingAllowedLicenses: [] as string[],
  };
  const imports: Record<string, string[]> = Object.fromEntries(
    releasePlatforms.map((platform) => [platform, ['fmt', 'github.com/misaon/nylzo/cmd/nylzo']]),
  );
  return { automation, crypto, cryptoAssessment, cryptoVulnerability, imports, policy, report, svelte, web, zod };
}

type Fixture = ReturnType<typeof fixture>;

function assess(
  data: Fixture,
  now: Date,
  policy: ScaPolicy = parseScaPolicy(data.policy),
  patches: readonly PatchAssessment[] = [],
): VexStatement[] {
  return assessSourceDependencies(parseDependencyReport(data.report), policy, data.imports, now, patches);
}

function withDeepmerge(data: Fixture, vulnerability: VulnerabilityEntry): PackageEntry {
  const deepmerge: PackageEntry = {
    licenses: ['MIT'],
    package: { ecosystem: 'npm', name: 'deepmerge', version: '4.3.1' },
    vulnerabilities: [vulnerability],
  };
  data.web.packages[0] = deepmerge;
  return deepmerge;
}

test('VEX cannot hide new versions, advisories, reachable code, missing platforms, or expired assessments', () => {
  const date = new Date('2026-10-27T00:00:00Z');
  assert.equal(assess(fixture(), date).length, 1);
  const mutations: ((data: Fixture) => void)[] = [
    (data) => (data.crypto.package.version = '0.58.1'),
    (data) => (data.cryptoVulnerability.id = 'GO-another'),
    (data) => (data.cryptoVulnerability.id = 'MAL-2026-123'),
    (data) => {
      data.imports['linux/amd64']?.push('golang.org/x/crypto/openpgp/packet');
    },
    (data) => delete data.imports['windows/arm64'],
    (data) => (data.cryptoAssessment.reviewUntil = '2026-10-26T00:00:00Z'),
    (data) => data.report.results.pop(),
    (data) => (data.svelte.licenses = []),
    (data) => (data.svelte.licenses = ['LicenseRef-Unknown']),
  ];
  for (const mutate of mutations) {
    const data = fixture();
    mutate(data);
    assert.throws(() => assess(data, date));
  }
});

test('development-tool license allowances do not broaden the application license policy', () => {
  const data = fixture();
  const date = new Date('2026-10-28T00:00:00Z');
  data.policy.toolingAllowedLicenses = ['CC-BY-3.0'];
  data.zod.licenses = ['CC-BY-3.0'];
  assert.equal(assess(data, date).length, 1);
  data.svelte.licenses = ['CC-BY-3.0'];
  assert.throws(() => assess(data, date));
});

test('a patched dependency is fixed only with exact, current installation evidence', () => {
  const date = new Date('2026-10-28T00:00:00Z');
  const data = fixture();
  const deepmerge = withDeepmerge(data, { id: deepmergePatch.id });
  const fixed = assess(data, date, repositoryPolicy, [deepmergePatch]);
  assert.equal(fixed.find((statement) => statement.vulnerability.name === deepmergePatch.id)?.status, 'fixed');
  assert.throws(() => assess(data, date, repositoryPolicy));
  assert.throws(() => assess(data, date, repositoryPolicy, [{ ...deepmergePatch, sha256: 'changed' }]));
  assert.throws(() => assess(data, new Date('2026-11-28T00:00:00Z'), repositoryPolicy, [deepmergePatch]));
  for (const id of ['GHSA-another', 'MAL-2026-123']) {
    const changed = fixture();
    withDeepmerge(changed, { id });
    assert.throws(() => assess(changed, date, repositoryPolicy, [deepmergePatch]));
  }
  const unpatchedTool = fixture();
  withDeepmerge(unpatchedTool, { id: deepmergePatch.id });
  unpatchedTool.automation.packages.push(structuredClone(deepmerge));
  assert.throws(
    () => assess(unpatchedTool, date, repositoryPolicy, [deepmergePatch]),
    'A web patch cannot exempt an unpatched copy in release tooling',
  );
  const changedPolicy = {
    ...repositoryPolicy,
    fixed: repositoryPolicy.fixed.map((assessment) => ({ ...assessment, patchSha256: 'changed' })),
  };
  assert.throws(() => assess(data, date, changedPolicy, [deepmergePatch]));
});

test('a malware alias cannot be excused as a patched vulnerability or a VEX exception', () => {
  const date = new Date('2026-10-28T00:00:00Z');
  const data = fixture();
  data.cryptoVulnerability.aliases = ['MAL-2026-123'];
  assert.throws(() => assess(data, date), /Known malware/);
  const patched = fixture();
  withDeepmerge(patched, { aliases: ['MAL-2026-456'], id: deepmergePatch.id });
  assert.throws(() => assess(patched, date, repositoryPolicy, [deepmergePatch]), /Known malware/);
});
