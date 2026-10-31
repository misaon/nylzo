export interface SemanticVersion {
  readonly major: number;
  readonly minor: number;
  readonly patch: number;
}

const releasePattern = /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;

export function parseVersion(value: string): SemanticVersion {
  const match = releasePattern.exec(value);
  if (!match) {
    throw new Error(`Not a stable semantic version: ${value}`);
  }
  const [major, minor, patch] = match.slice(1).map(Number);
  if (major === undefined || minor === undefined || patch === undefined) {
    throw new Error(`Not a stable semantic version: ${value}`);
  }
  return { major, minor, patch };
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
