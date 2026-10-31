import { actionPinPattern, actionPins } from '../../github/action-pins.ts';
import { compareSnapshots, type Extraction, type FileRule, snapshot, type UpstreamClaim } from './file-review.ts';

const workflowValues: readonly Extraction[] = [
  { name: 'action', pattern: actionPinPattern, values: { commit: 'opaque', version: 'version' } },
  {
    name: 'dependency',
    pattern:
      /^ *# renovate: datasource=[a-z-]+ depName=(?<dependency>[\w./-]+)\n *[a-z-]+: (?<version>v?\d+\.\d+\.\d+)$/dgm,
    values: { version: 'version' },
  },
  {
    name: 'govulncheck',
    pattern: /go run golang\.org\/x\/vuln\/cmd\/govulncheck@(?<version>v\d+\.\d+\.\d+) /dg,
    values: { version: 'version' },
  },
  {
    name: 'renovate',
    pattern:
      /^ *renovate-image: ghcr\.io\/renovatebot\/renovate:(?<version>\d+\.\d+\.\d+)@sha256:(?<digest>[a-f\d]{64})$/dgm,
    values: { digest: 'opaque', version: 'version' },
  },
];

function actionTagClaims(workflow: string): Map<string, UpstreamClaim> {
  return new Map(
    actionPins(workflow).map(({ action, commit, version }): [string, UpstreamClaim] => [
      `${action}@${commit} ${version}`,
      { commit, kind: 'action-tag', repository: action, tag: version },
    ]),
  );
}

export const workflow: FileRule = (path, before, after) => {
  const review = compareSnapshots(path, snapshot(before, workflowValues), snapshot(after, workflowValues));
  const existing = actionTagClaims(before);
  for (const [key, claim] of actionTagClaims(after)) {
    if (!existing.has(key)) {
      review.claims.push(claim);
    }
  }
  return review;
};
