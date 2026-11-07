import { compareVersions, formatTag, maintenanceBranch, parseVersion } from '../versions/semantic-version.ts';
import type { SemanticVersion } from '../versions/semantic-version.ts';

export type ReleaseKind = 'dependencies' | 'planned' | 'security';

export type MaintenanceTrigger = 'merge' | 'window';

export interface UnreleasedPullRequest {
  readonly labels: readonly string[];
  readonly number: number;
}

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
  readonly trigger: MaintenanceTrigger;
  readonly unreleased: readonly UnreleasedPullRequest[];
}

// A security fix releases after its own CI; everything else waits for the next dependency release window.
export function maintenanceKind(
  trigger: MaintenanceTrigger,
  unreleased: readonly UnreleasedPullRequest[],
): ReleaseKind | null {
  if (unreleased.some((pull) => pull.labels.includes('security'))) {
    return 'security';
  }
  return trigger === 'window' && unreleased.length > 0 ? 'dependencies' : null;
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
  const kind = maintenanceKind(input.trigger, input.unreleased);
  if (input.commit === input.latestReleaseCommit || kind === null) {
    return null;
  }
  return {
    baseRelease: input.latestRelease,
    commit: input.commit,
    kind,
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
