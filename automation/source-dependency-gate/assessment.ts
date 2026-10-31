import assert from 'node:assert/strict';

import type { Component, DependencyReport, LockedPackage, LockfileReport, Vulnerability } from './osv-report.ts';
import type { Assessment, FixedAssessment, NotAffectedAssessment, ScaPolicy } from './sca-policy.ts';

import { deepmergePatch, isSamePatch, type PatchAssessment } from '../web-install/deepmerge-patch.ts';
import { goBuilds, type GoImports } from './go-imports.ts';

export type VexStatement = FixedStatement | NotAffectedStatement;

interface FixedStatement {
  readonly component: string;
  readonly impact_statement: string;
  readonly lockfile: string;
  readonly status: 'fixed';
  readonly vulnerability: { readonly name: string };
}

interface NotAffectedStatement {
  readonly component: string;
  readonly impact_statement: string;
  readonly justification: string;
  readonly lockfile: string;
  readonly status: 'not_affected';
  readonly vulnerability: { readonly name: string };
}

export const scannedLockfiles = [
  'go.mod',
  'web/pnpm-lock.yaml',
  'automation/pnpm-lock.yaml',
  'deploy/caddy/go.mod',
] as const;

type ScannedLockfile = (typeof scannedLockfiles)[number];

export const patchedLockfile: ScannedLockfile = 'web/pnpm-lock.yaml';
const toolingLockfile: ScannedLockfile = 'automation/pnpm-lock.yaml';
const reviewedForbiddenImport = 'golang.org/x/crypto/openpgp';
const reviewedJustification = 'vulnerable_code_not_present';

export function assessSourceDependencies(
  report: DependencyReport,
  policy: ScaPolicy,
  imports: GoImports,
  now: Date,
  verifiedPatches: readonly PatchAssessment[] = [],
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
    [...policy.fixed, ...policy.notAffected].every((assessment) => assessment.lockfile !== undefined),
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
      if (!hasAllowedLicenses(locked, allowedLicenses)) {
        findings.push(`License review required: ${component.name}@${component.version}`);
      }
      for (const vulnerability of locked.vulnerabilities) {
        if (malware.includes(vulnerability)) {
          continue;
        }
        const fixed =
          lockfile === patchedLockfile ? matching(policy.fixed, lockfile, component, vulnerability) : undefined;
        if (fixed !== undefined) {
          statements.push(fixedStatement(fixed, component, now, verifiedPatches));
          continue;
        }
        const notAffected = matching(policy.notAffected, lockfile, component, vulnerability);
        if (notAffected === undefined) {
          findings.push(`Vulnerability: ${component.name}@${component.version} ${vulnerability.id}`);
          continue;
        }
        statements.push(notAffectedStatement(notAffected, lockfile, component, now, imports));
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

function matching<Entry extends Assessment>(
  assessments: readonly Entry[],
  lockfile: ScannedLockfile,
  component: Component,
  vulnerability: Vulnerability,
): Entry | undefined {
  return assessments.find(
    (assessment) =>
      assessment.lockfile === lockfile &&
      assessment.id === vulnerability.id &&
      assessment.package === component.name &&
      assessment.version === component.version &&
      assessment.ecosystem === component.ecosystem,
  );
}

function fixedStatement(
  assessment: FixedAssessment,
  component: Component,
  now: Date,
  verifiedPatches: readonly PatchAssessment[],
): FixedStatement {
  assert.ok(
    isSamePatch({ ...assessment, sha256: assessment.patchSha256 }, deepmergePatch),
    'Only the reviewed deepmerge remediation is supported',
  );
  assert.ok(isUnderReview(assessment, now), 'Patch assessment expired');
  assert.ok(
    verifiedPatches.some((patch) => isSamePatch(patch, deepmergePatch)),
    'Missing installed patch and regression evidence',
  );
  return {
    vulnerability: { name: assessment.id },
    status: 'fixed',
    impact_statement: assessment.impact,
    component: `pkg:npm/${component.name}@${component.version}`,
    lockfile: patchedLockfile,
  };
}

function notAffectedStatement(
  assessment: NotAffectedAssessment,
  lockfile: ScannedLockfile,
  component: Component,
  now: Date,
  imports: GoImports,
): NotAffectedStatement {
  assert.equal(assessment.justification, reviewedJustification, 'Only absent vulnerable code can be not affected');
  assert.ok(isUnderReview(assessment, now), 'VEX assessment expired');
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
  return {
    vulnerability: { name: assessment.id },
    status: 'not_affected',
    justification: assessment.justification,
    impact_statement: assessment.impact,
    component: `pkg:golang/${component.name}@v${component.version}`,
    lockfile,
  };
}

function hasExactly(values: readonly string[], expected: readonly string[]): boolean {
  return values.length === expected.length && expected.every((value) => values.includes(value));
}

function isUnderReview(assessment: Assessment, now: Date): boolean {
  return Date.parse(assessment.reviewUntil) > now.getTime();
}
