import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { parseArgs } from 'node:util';

import { verifyDeepmergePatch } from '../web-install/deepmerge-patch.ts';
import { assessSourceDependencies, scannedLockfiles } from './assessment.ts';
import { collectGoImports } from './go-imports.ts';
import { arrayAt, objectAt } from './json-values.ts';
import { openVexDocument, openVexFileName, type Release } from './openvex.ts';
import { hasVulnerabilities, lockedPackageCount, parseDependencyReport } from './osv-report.ts';
import { parseScaPolicy } from './sca-policy.ts';

const repositoryRoot = path.resolve(import.meta.dirname, '../..');

const { positionals, values } = parseArgs({
  allowPositionals: true,
  options: { sbom: { default: false, type: 'boolean' } },
});
const reportDirectory = path.resolve(positionals[0] ?? '.sca-reports');
mkdirSync(reportDirectory, { recursive: true });

const release = requestedRelease();
const policy = parseScaPolicy(readJson(path.join(repositoryRoot, 'security/sca-policy.json')));
const patchEvidence = [verifyDeepmergePatch()];
const scannedLicenses = [...policy.allowedLicenses, ...policy.toolingAllowedLicenses].filter(
  (license) => !license.includes(' OR '),
);
const scannerArguments = [
  'scan',
  'source',
  '--config=security/osv-scanner.toml',
  ...scannedLockfiles.map((lockfile) => `--lockfile=${lockfile}`),
  '--all-packages',
  '--all-vulns',
  `--licenses=${scannedLicenses.join(',')}`,
];

const reportPath = path.join(reportDirectory, 'source.json');
runScanner(['--format=json', `--output-file=${reportPath}`], 'Dependency scanner failed');
const report = parseDependencyReport(readJson(reportPath));
const imports = hasVulnerabilities(report) ? collectGoImports(repositoryRoot) : {};
writeJson(path.join(reportDirectory, 'go-imports.json'), imports);

const now = new Date();
const statements = assessSourceDependencies(report, policy, imports, now, patchEvidence);
writeJson(path.join(reportDirectory, 'patch-evidence.json'), patchEvidence);

const commit = git('rev-parse', 'HEAD');
assert.match(commit, /^[\da-f]{40}$/, 'HEAD must be a full commit hash');
if (statements.length > 0 && isCleanCheckout()) {
  writeJson(
    path.join(reportDirectory, openVexFileName(release)),
    openVexDocument(statements, { commit, release }, now),
  );
}
if (values.sbom) {
  writeSourceSbom(release?.version ?? commit);
}
console.log(`Checked ${lockedPackageCount(report)} locked packages; ${statements.length} exact VEX assessment(s).`);

function requestedRelease(): Release | undefined {
  const requestedVersion = process.env['NYLZO_VERSION'];
  if (requestedVersion === undefined || requestedVersion === '') {
    return undefined;
  }
  assert.match(requestedVersion, /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/, 'NYLZO_VERSION must be X.Y.Z');
  assert.ok(isCleanCheckout(), 'Release evidence requires a clean checkout');
  const version = requestedVersion.replace(/^v/, '');
  if (process.env['GITHUB_ACTIONS'] === 'true') {
    assert.equal(process.env['GITHUB_REF_NAME'], `v${version}`, 'NYLZO_VERSION must name the pushed tag');
  }
  return { requestedVersion, version };
}

function runScanner(outputArguments: readonly string[], failure: string): void {
  const scan = spawnSync('osv-scanner', [...scannerArguments, ...outputArguments], {
    cwd: repositoryRoot,
    stdio: 'inherit',
  });
  assert.ok(scan.status === 0 || scan.status === 1, failure);
}

function writeSourceSbom(identity: string): void {
  const sbomPath = path.join(reportDirectory, `nylzo_${identity}.source.spdx.json`);
  runScanner(['--format=spdx-2-3', `--output-file=${sbomPath}`], 'Source SBOM generation failed');
  const sbom = objectAt(readJson(sbomPath), 'source SBOM');
  assert.equal(sbom['spdxVersion'], 'SPDX-2.3', 'The source SBOM must be SPDX 2.3');
  assert.ok(
    arrayAt(sbom['packages'], 'source SBOM packages').length >= lockedPackageCount(report),
    'The source SBOM must list every locked package',
  );
}

function isCleanCheckout(): boolean {
  return git('status', '--porcelain', '--untracked-files=no') === '';
}

function git(...gitArguments: string[]): string {
  return execFileSync('git', gitArguments, { cwd: repositoryRoot, encoding: 'utf8' }).trim();
}

function readJson(file: string): unknown {
  return JSON.parse(readFileSync(file, 'utf8'));
}

function writeJson(file: string, value: unknown): void {
  writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`);
}
