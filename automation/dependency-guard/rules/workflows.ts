import { compareSnapshots, type Extraction, type FileRule, snapshot, type UpstreamClaim } from './file-review.ts';

const actionPin =
  /^ *(?:- )?uses: (?<action>[\w.-]+\/[\w.-]+)(?:\/[\w./-]+)?@(?<commit>[a-f\d]{40}) # (?<version>v?\d+\.\d+\.\d+)$/dgm;

const workflowValues: readonly Extraction[] = [
  { name: 'action', pattern: actionPin, values: { commit: 'opaque', version: 'version' } },
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

function actionPins(workflow: string): Map<string, UpstreamClaim> {
  const pins = new Map<string, UpstreamClaim>();
  for (const match of workflow.matchAll(actionPin)) {
    const { action = '', commit = '', version = '' } = match.groups ?? {};
    pins.set(`${action}@${commit} ${version}`, { commit, kind: 'action-tag', repository: action, tag: version });
  }
  return pins;
}

export const workflow: FileRule = (path, before, after) => {
  const review = compareSnapshots(path, snapshot(before, workflowValues), snapshot(after, workflowValues));
  const existing = actionPins(before);
  for (const [key, claim] of actionPins(after)) {
    if (!existing.has(key)) {
      review.claims.push(claim);
    }
  }
  return review;
};
