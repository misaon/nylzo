export interface SemanticVersion {
  readonly major: number;
  readonly minor: number;
  readonly patch: number;
}

const releasePattern = /^v?(0|[1-9]\d{0,14})\.(0|[1-9]\d{0,14})\.(0|[1-9]\d{0,14})$/u;

export function stableVersion(value: string): SemanticVersion | undefined {
  const match = releasePattern.exec(value);
  return match ? { major: Number(match[1]), minor: Number(match[2]), patch: Number(match[3]) } : undefined;
}

export function parseVersion(value: string): SemanticVersion {
  const version = stableVersion(value);
  if (version === undefined) {
    throw new Error(`Not a stable semantic version: ${value}`);
  }
  return version;
}

export function isReleaseTag(value: string): boolean {
  return value.startsWith('v') && releasePattern.test(value);
}

export function formatTag(version: SemanticVersion): string {
  return `v${version.major}.${version.minor}.${version.patch}`;
}

export function compareVersions(left: SemanticVersion, right: SemanticVersion): number {
  return left.major - right.major || left.minor - right.minor || left.patch - right.patch;
}

export function isSafeUpgrade(before: SemanticVersion, after: SemanticVersion): boolean {
  const sameCompatibilityLine = after.major === before.major && (before.major > 0 || after.minor === before.minor);
  return sameCompatibilityLine && compareVersions(after, before) > 0;
}

export function maintenanceBranch(version: SemanticVersion): string {
  return `release/v${version.major}.${version.minor}`;
}
