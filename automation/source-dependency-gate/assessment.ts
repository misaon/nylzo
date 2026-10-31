import assert from 'node:assert/strict';

import type { Component, DependencyReport, LockedPackage, LockfileReport, Vulnerability } from './osv-report.ts';
import type { Assessment, FixedAssessment, NotAffectedAssessment, ScaPolicy } from './sca-policy.ts';

import { deepmergePatch, isSamePatch, type PatchAssessment } from '../web-install/deepmerge-patch.ts';
import { type GoImports, releasePlatforms } from './go-imports.ts';

export type VexStatement = FixedStatement | NotAffectedStatement;

interface FixedStatement {
  readonly component: string;
  readonly impact_statement: string;
  readonly status: 'fixed';
  readonly vulnerability: { readonly name: string };
}

interface NotAffectedStatement {
  readonly component: string;
  readonly impact_statement: string;
  readonly justification: string;
  readonly status: 'not_affected';
  readonly vulnerability: { readonly name: string };
}

export const scannedLockfiles = ['go.mod', 'web/pnpm-lock.yaml', 'automation/pnpm-lock.yaml'] as const;

type ScannedLockfile = (typeof scannedLockfiles)[number];

export const patchedLockfile: ScannedLockfile = 'web/pnpm-lock.yaml';
const toolingLockfile: ScannedLockfile = 'automation/pnpm-lock.yaml';
const commandPackage = 'github.com/misaon/nylzo/cmd/nylzo';
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
    'Application and release-tooling lockfiles must all be scanned',
  );
  assert.ok(policy.allowedLicenses.length > 0, 'The SCA policy allows no licenses');
  const findings: string[] = [];
  const statements: VexStatement[] = [];
  for (const result of report.results) {
    const lockfile = lockfileOf(result);
    assert.ok(result.packages.length > 0, 'Empty dependency inventory');
    const allowedLicenses = new Set(
      lockfile === toolingLockfile
        ? [...policy.allowedLicenses, ...policy.toolingAllowedLicenses]
        : policy.allowedLicenses,
    );
    for (const locked of result.packages) {
      const { component } = locked;
      if (!hasAllowedLicenses(locked, allowedLicenses)) {
        findings.push(`License review required: ${component.name}@${component.version}`);
      }
      for (const vulnerability of locked.vulnerabilities) {
        if (isMalware(vulnerability)) {
          findings.push(`Known malware: ${component.name} ${vulnerability.id}`);
          continue;
        }
        const fixed = lockfile === patchedLockfile ? matching(policy.fixed, component, vulnerability) : undefined;
        if (fixed !== undefined) {
          statements.push(fixedStatement(fixed, component, now, verifiedPatches));
          continue;
        }
        const notAffected = matching(policy.notAffected, component, vulnerability);
        if (notAffected === undefined) {
          findings.push(`Vulnerability: ${component.name}@${component.version} ${vulnerability.id}`);
          continue;
        }
        statements.push(notAffectedStatement(notAffected, component, now, imports));
      }
    }
  }
  assert.equal(findings.length, 0, findings.join('\n'));
  return statements;
}

function lockfileOf(result: LockfileReport): ScannedLockfile {
  const lockfile = scannedLockfiles.find((candidate) => result.path.endsWith(`/${candidate}`));
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
  component: Component,
  vulnerability: Vulnerability,
): Entry | undefined {
  return assessments.find(
    (assessment) =>
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
  };
}

function notAffectedStatement(
  assessment: NotAffectedAssessment,
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
  assert.ok(hasExactly(Object.keys(imports), releasePlatforms), 'All release targets need import evidence');
  for (const platform of releasePlatforms) {
    const packages = imports[platform] ?? [];
    assert.ok(packages.includes(commandPackage), 'Missing complete application import inventory');
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
  };
}

function hasExactly(values: readonly string[], expected: readonly string[]): boolean {
  return values.length === expected.length && expected.every((value) => values.includes(value));
}

function isUnderReview(assessment: Assessment, now: Date): boolean {
  return Date.parse(assessment.reviewUntil) > now.getTime();
}
