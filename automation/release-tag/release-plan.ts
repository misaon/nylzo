import {
  compareVersions,
  formatTag,
  maintenanceBranch,
  parseVersion,
  type SemanticVersion,
} from '../versions/semantic-version.ts';

export type ReleaseKind = 'planned' | 'security';

export interface ReleasePlan {
  readonly baseRelease: string;
  readonly commit: string;
  readonly kind: ReleaseKind;
  readonly tag: string;
}

export interface MaintenanceInput {
  readonly branch: string;
  readonly commit: string;
  readonly latestRelease: string;
  readonly latestReleaseCommit: string;
}

export interface PlannedInput {
  readonly commit: string;
  readonly latestRelease: string;
  readonly manifestVersion: string;
}

export function maintenanceRelease(input: MaintenanceInput): ReleasePlan | null {
  const latest = parseVersion(input.latestRelease);
  if (input.branch !== maintenanceBranch(latest)) {
    throw new Error(`${input.branch} is not the maintenance branch of the latest release ${input.latestRelease}`);
  }
  if (input.commit === input.latestReleaseCommit) {
    return null;
  }
  return {
    baseRelease: input.latestRelease,
    commit: input.commit,
    kind: 'security',
    tag: formatTag({ ...latest, patch: latest.patch + 1 }),
  };
}

export function plannedRelease(input: PlannedInput): ReleasePlan {
  const planned: SemanticVersion = parseVersion(input.manifestVersion);
  const latest = parseVersion(input.latestRelease);
  if (planned.patch !== 0 || compareVersions(planned, latest) <= 0) {
    throw new Error(`A planned release must raise the minor or major version above ${input.latestRelease}`);
  }
  return { baseRelease: input.latestRelease, commit: input.commit, kind: 'planned', tag: formatTag(planned) };
}
