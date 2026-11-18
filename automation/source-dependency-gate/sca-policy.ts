import { arrayAt, objectAt, optionalArrayAt, textAt, textsAt } from './json-values.ts';
import type { JsonObject } from './json-values.ts';

export interface Assessment {
  readonly ecosystem: string;
  readonly id: string;
  readonly impact: string;
  readonly lockfile?: string;
  readonly package: string;
  readonly reviewUntil: string;
  readonly version: string;
}

export interface NotAffectedAssessment extends Assessment {
  readonly forbiddenImport?: string;
  readonly justification: string;
}

export interface ScaPolicy {
  readonly allowedLicenses: readonly string[];
  readonly notAffected: readonly NotAffectedAssessment[];
  readonly toolingAllowedLicenses: readonly string[];
}

export function parseScaPolicy(value: unknown): ScaPolicy {
  const policy = objectAt(value, 'policy');
  const notAffected = arrayAt(policy['notAffected'], 'policy.notAffected');
  return {
    allowedLicenses: textsAt(policy['allowedLicenses'], 'policy.allowedLicenses'),
    notAffected: notAffected.map((entry, index) => parseNotAffected(entry, `policy.notAffected[${index}]`)),
    toolingAllowedLicenses: textsAt(
      optionalArrayAt(policy['toolingAllowedLicenses'], 'policy.toolingAllowedLicenses'),
      'policy.toolingAllowedLicenses',
    ),
  };
}

function parseNotAffected(value: unknown, location: string): NotAffectedAssessment {
  const entry = objectAt(value, location);
  return {
    ...parseAssessment(entry, location),
    ...(entry['forbiddenImport'] !== undefined && {
      forbiddenImport: textAt(entry['forbiddenImport'], `${location}.forbiddenImport`),
    }),
    justification: textAt(entry['justification'], `${location}.justification`),
  };
}

function parseAssessment(entry: JsonObject, location: string): Assessment {
  return {
    ecosystem: textAt(entry['ecosystem'], `${location}.ecosystem`),
    id: textAt(entry['id'], `${location}.id`),
    impact: textAt(entry['impact'], `${location}.impact`),
    ...(entry['lockfile'] !== undefined && { lockfile: textAt(entry['lockfile'], `${location}.lockfile`) }),
    package: textAt(entry['package'], `${location}.package`),
    reviewUntil: textAt(entry['reviewUntil'], `${location}.reviewUntil`),
    version: textAt(entry['version'], `${location}.version`),
  };
}
