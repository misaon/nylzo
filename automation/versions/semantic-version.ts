export interface SemanticVersion {
  readonly major: number;
  readonly minor: number;
  readonly patch: number;
}

const releasePattern = /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u;

export function stableVersion(value: string): SemanticVersion | undefined {
  const match = releasePattern.exec(value);
  if (!match) {
    return undefined;
  }
  const version = { major: Number(match[1]), minor: Number(match[2]), patch: Number(match[3]) };
  return Object.values(version).every((part) => Number.isSafeInteger(part)) ? version : undefined;
}

export function parseVersion(value: string): SemanticVersion {
  const version = stableVersion(value);
  if (version === undefined) {
    throw new Error(`Not a stable semantic version: ${value}`);
  }
  return version;
}

export function isReleaseTag(value: string): boolean {
  return value.startsWith('v') && stableVersion(value) !== undefined;
}

export function formatTag(version: SemanticVersion): string {
  return `v${version.major}.${version.minor}.${version.patch}`;
}

export function compareVersions(left: SemanticVersion, right: SemanticVersion): number {
  return left.major - right.major || left.minor - right.minor || left.patch - right.patch;
}

export function maintenanceBranch(version: SemanticVersion): string {
  return `release/v${version.major}.${version.minor}`;
}
