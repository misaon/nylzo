import assert from 'node:assert/strict';

import { localWorkspaces } from '../web-install/policy.ts';
import { goBuilds } from './go-imports.ts';
import type { GoImports } from './go-imports.ts';
import type { Component, DependencyReport, LockedPackage, LockfileReport, Vulnerability } from './osv-report.ts';
import type { NpmRuntimeEvidence } from './runtime-dependencies.ts';
import type { Assessment, NotAffectedAssessment, ScaPolicy } from './sca-policy.ts';

export interface VexStatement {
  readonly component: string;
  readonly impact_statement: string;
  readonly justification: string;
  readonly lockfile: string;
  readonly status: 'not_affected';
  readonly vulnerability: { readonly name: string };
}

export const scannedLockfiles = ['go.mod', 'web/bun.lock', 'automation/bun.lock', 'deploy/caddy/go.mod'] as const;

type ScannedLockfile = (typeof scannedLockfiles)[number];

export const npmLockfiles = ['web/bun.lock', 'automation/bun.lock'] as const satisfies readonly ScannedLockfile[];

export type NpmRuntime = Partial<Record<ScannedLockfile, NpmRuntimeEvidence>>;

const toolingLockfile: ScannedLockfile = 'automation/bun.lock';
const reviewedForbiddenImport = 'golang.org/x/crypto/openpgp';
const reviewedJustification = 'vulnerable_code_not_present';
const buildToolJustification = 'vulnerable_code_cannot_be_controlled_by_adversary';

export function assessSourceDependencies(
  report: DependencyReport,
  policy: ScaPolicy,
  imports: GoImports,
  now: Date,
  npmRuntime: NpmRuntime = {},
): VexStatement[] {
  assert.ok(
    hasExactly(
      report.results.map((result) => lockfileOf(result)),
      scannedLockfiles,
    ),
    'Application, Caddy and release-tooling lockfiles must all be scanned',
  );
  assert.ok(policy.allowedLicenses.length > 0, 'The SCA policy allows no licenses');
  assert.ok(
    policy.notAffected.every((assessment) => assessment.lockfile !== undefined),
    'Every SCA assessment must name the lockfile it covers',
  );
  const findings: string[] = [];
  const statements: VexStatement[] = [];
  for (const result of report.results) {
    const lockfile = lockfileOf(result);
    assert.ok(result.packages.length > 0, 'Empty dependency inventory');
    const shipped = linkedModules(lockfile, imports);
    const allowedLicenses = new Set(
      lockfile === toolingLockfile
        ? [...policy.allowedLicenses, ...policy.toolingAllowedLicenses]
        : policy.allowedLicenses,
    );
    for (const locked of result.packages) {
      const { component } = locked;
      const malware = locked.vulnerabilities.filter((vulnerability) => isMalware(vulnerability));
      for (const vulnerability of malware) {
        findings.push(`Known malware: ${component.name} ${vulnerability.id}`);
      }
      if (shipped !== undefined && !shipped.has(component.name)) {
        continue;
      }
      // Bun inventories local links as workspace:<directory>; these are this repository's MIT source, not registry packages.
      const localDirectory = localWorkspaces[component.name];
      const licensedLocalWorkspace =
        lockfile === 'web/bun.lock' &&
        component.ecosystem === 'npm' &&
        localDirectory !== undefined &&
        component.version === `workspace:${localDirectory}` &&
        allowedLicenses.has('MIT');
      if (!licensedLocalWorkspace && !hasAllowedLicenses(locked, allowedLicenses)) {
        findings.push(`License review required: ${component.name}@${component.version}`);
      }
      for (const vulnerability of locked.vulnerabilities) {
        if (malware.includes(vulnerability)) {
          continue;
        }
        const notAffected = matching(policy.notAffected, lockfile, component, vulnerability);
        if (notAffected === undefined) {
          findings.push(`Vulnerability: ${component.name}@${component.version} ${vulnerability.id}`);
          continue;
        }
        statements.push(notAffectedStatement(notAffected, lockfile, component, now, imports, npmRuntime));
      }
    }
  }
  assert.equal(findings.length, 0, findings.join('\n'));
  return statements;
}

function linkedModules(lockfile: ScannedLockfile, imports: GoImports): ReadonlySet<string> | undefined {
  const build = goBuilds[lockfile];
  if (build === undefined || build.reviewsUnlinkedModules) {
    return undefined;
  }
  const evidence = imports[lockfile] ?? {};
  assert.ok(hasExactly(Object.keys(evidence), build.platforms), `Every ${lockfile} release target needs link evidence`);
  return new Set(
    build.platforms.flatMap((platform) => {
      const linked = evidence[platform];
      assert.ok(linked, `Missing ${lockfile} link evidence for ${platform}`);
      assert.ok(linked.packages.includes(build.command), `Missing complete ${lockfile} import inventory`);
      return linked.modules;
    }),
  );
}

function lockfileOf(result: LockfileReport): ScannedLockfile {
  const [lockfile] = scannedLockfiles
    .filter((candidate) => result.path.endsWith(`/${candidate}`))
    .toSorted((left, right) => right.length - left.length);
  assert.ok(lockfile !== undefined, `Unexpected lockfile in the scan: ${result.path}`);
  return lockfile;
}

function hasAllowedLicenses(locked: LockedPackage, allowedLicenses: ReadonlySet<string>): boolean {
  return (
    locked.licenses.length > 0 &&
    locked.licenses.every((license) => allowedLicenses.has(license)) &&
    locked.licenseViolations === 0
  );
}

function isMalware(vulnerability: Vulnerability): boolean {
  return [vulnerability.id, ...vulnerability.aliases].some((id) => id.startsWith('MAL-'));
}

function matching(
  assessments: readonly NotAffectedAssessment[],
  lockfile: ScannedLockfile,
  component: Component,
  vulnerability: Vulnerability,
): NotAffectedAssessment | undefined {
  return assessments.find(
    (assessment) =>
      assessment.lockfile === lockfile &&
      assessment.id === vulnerability.id &&
      assessment.package === component.name &&
      assessment.version === component.version &&
      assessment.ecosystem === component.ecosystem,
  );
}

function notAffectedStatement(
  assessment: NotAffectedAssessment,
  lockfile: ScannedLockfile,
  component: Component,
  now: Date,
  imports: GoImports,
  npmRuntime: NpmRuntime,
): VexStatement {
  assert.ok(isUnderReview(assessment, now), 'VEX assessment expired');
  const purl =
    assessment.justification === buildToolJustification
      ? buildToolPackage(assessment, lockfile, component, npmRuntime)
      : absentOpenPgpModule(assessment, lockfile, component, imports);
  return {
    vulnerability: { name: assessment.id },
    status: 'not_affected',
    justification: assessment.justification,
    impact_statement: assessment.impact,
    component: purl,
    lockfile,
  };
}

function absentOpenPgpModule(
  assessment: NotAffectedAssessment,
  lockfile: ScannedLockfile,
  component: Component,
  imports: GoImports,
): string {
  assert.equal(
    assessment.justification,
    reviewedJustification,
    'Only absent vulnerable code or build-only packages can be not affected',
  );
  assert.equal(
    assessment.forbiddenImport,
    reviewedForbiddenImport,
    'Only the reviewed OpenPGP assessment is supported',
  );
  const build = goBuilds[lockfile];
  assert.ok(build !== undefined, 'Only Go code can lack the vulnerable import');
  const evidence = imports[lockfile] ?? {};
  assert.ok(hasExactly(Object.keys(evidence), build.platforms), 'All release targets need import evidence');
  for (const platform of build.platforms) {
    const packages = evidence[platform]?.packages ?? [];
    assert.ok(packages.includes(build.command), 'Missing complete application import inventory');
    assert.ok(
      packages.every((path) => path !== reviewedForbiddenImport && !path.startsWith(`${reviewedForbiddenImport}/`)),
      `Vulnerable OpenPGP code is present for ${platform}`,
    );
  }
  return `pkg:golang/${component.name}@v${component.version}`;
}

function buildToolPackage(
  assessment: NotAffectedAssessment,
  lockfile: ScannedLockfile,
  component: Component,
  npmRuntime: NpmRuntime,
): string {
  assert.equal(assessment.forbiddenImport, undefined, 'A build-only assessment names no import');
  assert.ok(
    component.ecosystem === 'npm' && npmLockfiles.some((npmLockfile) => npmLockfile === lockfile),
    'Only an npm package can be build-only',
  );
  const evidence = npmRuntime[lockfile];
  assert.ok(evidence !== undefined, `Missing runtime dependency evidence for ${lockfile}`);
  const locked = `${component.name}@${component.version}`;
  assert.ok(evidence.locked.includes(locked), `${locked} is not locked in ${lockfile}`);
  assert.ok(!evidence.runtime.includes(locked), `${locked} is a runtime dependency in ${lockfile}`);
  return `pkg:npm/${component.name}@${component.version}`;
}

function hasExactly(values: readonly string[], expected: readonly string[]): boolean {
  return values.length === expected.length && expected.every((value) => values.includes(value));
}

function isUnderReview(assessment: Assessment, now: Date): boolean {
  return Date.parse(assessment.reviewUntil) > now.getTime();
}
