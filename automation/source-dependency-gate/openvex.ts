import type { VexStatement } from './assessment.ts';

import { goBuilds } from './go-imports.ts';

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
  return {
    '@context': 'https://openvex.dev/ns/v0.2.0',
    '@id': `${repositoryUrl}/commit/${subject.commit}#vex-${subject.release?.requestedVersion ?? 'source'}`,
    author: repositoryUrl,
    timestamp: timestamp.toISOString(),
    version: 1,
    statements: statements.map(({ component, lockfile, ...statement }) => ({
      ...statement,
      products: [{ '@id': productOf(subject, lockfile), subcomponents: [{ '@id': component }] }],
    })),
  };
}

function productOf(subject: EvidenceSubject, lockfile: string): string {
  const directory = goBuilds[lockfile]?.directory ?? '.';
  if (subject.release === undefined) {
    const tree = `${repositoryUrl}/tree/${subject.commit}`;
    return directory === '.' ? tree : `${tree}/${directory}`;
  }
  const release = `pkg:golang/github.com/misaon/nylzo@v${subject.release.version}`;
  return directory === '.' ? release : `${release}#${directory}`;
}

export function openVexFileName(release: Release | undefined): string {
  return release === undefined ? 'nylzo.openvex.json' : `nylzo_${release.version}.openvex.json`;
}
