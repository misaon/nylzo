import { arrayAt, type JsonObject, objectAt, optionalArrayAt, textAt, textsAt } from './json-values.ts';

export interface Assessment {
  readonly ecosystem: string;
  readonly id: string;
  readonly impact: string;
  readonly package: string;
  readonly reviewUntil: string;
  readonly version: string;
}

export interface FixedAssessment extends Assessment {
  readonly patchSha256: string;
}

export interface NotAffectedAssessment extends Assessment {
  readonly forbiddenImport: string;
  readonly justification: string;
}

export interface ScaPolicy {
  readonly allowedLicenses: readonly string[];
  readonly fixed: readonly FixedAssessment[];
  readonly notAffected: readonly NotAffectedAssessment[];
  readonly toolingAllowedLicenses: readonly string[];
}

export function parseScaPolicy(value: unknown): ScaPolicy {
  const policy = objectAt(value, 'policy');
  const fixed = optionalArrayAt(policy['fixed'], 'policy.fixed');
  const notAffected = arrayAt(policy['notAffected'], 'policy.notAffected');
  return {
    allowedLicenses: textsAt(policy['allowedLicenses'], 'policy.allowedLicenses'),
    fixed: fixed.map((entry, index) => parseFixed(entry, `policy.fixed[${index}]`)),
    notAffected: notAffected.map((entry, index) => parseNotAffected(entry, `policy.notAffected[${index}]`)),
    toolingAllowedLicenses: textsAt(
      optionalArrayAt(policy['toolingAllowedLicenses'], 'policy.toolingAllowedLicenses'),
      'policy.toolingAllowedLicenses',
    ),
  };
}

function parseFixed(value: unknown, location: string): FixedAssessment {
  const entry = objectAt(value, location);
  return { ...parseAssessment(entry, location), patchSha256: textAt(entry['patchSha256'], `${location}.patchSha256`) };
}

function parseNotAffected(value: unknown, location: string): NotAffectedAssessment {
  const entry = objectAt(value, location);
  return {
    ...parseAssessment(entry, location),
    forbiddenImport: textAt(entry['forbiddenImport'], `${location}.forbiddenImport`),
    justification: textAt(entry['justification'], `${location}.justification`),
  };
}

function parseAssessment(entry: JsonObject, location: string): Assessment {
  return {
    ecosystem: textAt(entry['ecosystem'], `${location}.ecosystem`),
    id: textAt(entry['id'], `${location}.id`),
    impact: textAt(entry['impact'], `${location}.impact`),
    package: textAt(entry['package'], `${location}.package`),
    reviewUntil: textAt(entry['reviewUntil'], `${location}.reviewUntil`),
    version: textAt(entry['version'], `${location}.version`),
  };
}
