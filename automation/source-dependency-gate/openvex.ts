import type { VexStatement } from './assessment.ts';

export interface EvidenceSubject {
  readonly commit: string;
  readonly release: Release | undefined;
}

export interface Release {
  readonly requestedVersion: string;
  readonly version: string;
}

const repositoryUrl = 'https://github.com/misaon/nylzo';

export function openVexDocument(
  statements: readonly VexStatement[],
  subject: EvidenceSubject,
  timestamp: Date,
): object {
  const product =
    subject.release === undefined
      ? `${repositoryUrl}/tree/${subject.commit}`
      : `pkg:golang/github.com/misaon/nylzo@v${subject.release.version}`;
  return {
    '@context': 'https://openvex.dev/ns/v0.2.0',
    '@id': `${repositoryUrl}/commit/${subject.commit}#vex-${subject.release?.requestedVersion ?? 'source'}`,
    author: repositoryUrl,
    timestamp: timestamp.toISOString(),
    version: 1,
    statements: statements.map(({ component, ...statement }) => ({
      ...statement,
      products: [{ '@id': product, subcomponents: [{ '@id': component }] }],
    })),
  };
}

export function openVexFileName(release: Release | undefined): string {
  return release === undefined ? 'nylzo.openvex.json' : `nylzo_${release.version}.openvex.json`;
}
