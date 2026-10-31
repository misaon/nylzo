export interface VersionUpdate {
  readonly name: string;
  readonly before: string;
  readonly after: string;
}

export type UpstreamClaim =
  | {
      readonly kind: 'action-tag';
      readonly repository: string;
      readonly tag: string;
      readonly commit: string;
    }
  | {
      readonly kind: 'release-asset';
      readonly repository: string;
      readonly tag: string;
      readonly asset: string;
      readonly digest: string;
    };

export interface FileReview {
  readonly findings: string[];
  readonly updates: VersionUpdate[];
  readonly claims: UpstreamClaim[];
}

export type FileRule = (path: string, before: string, after: string) => FileReview;

export type ValueKind = 'fixed' | 'opaque' | 'version';

export interface DependencyValue {
  readonly name: string;
  readonly kind: ValueKind;
  readonly value: string;
}

export interface Snapshot {
  readonly template: string;
  readonly values: readonly DependencyValue[];
}

export interface Extraction {
  readonly pattern: RegExp;
  readonly values: Readonly<Record<string, ValueKind>>;
  readonly name: string;
}

export function emptyReview(): FileReview {
  return { claims: [], findings: [], updates: [] };
}

export function placeholder(kind: ValueKind): string {
  return `⟪${kind}⟫`;
}

export function snapshot(text: string, extractions: readonly Extraction[]): Snapshot {
  let template = text;
  const values: DependencyValue[] = [];
  for (const extraction of extractions) {
    if (!extraction.pattern.global || !extraction.pattern.hasIndices) {
      throw new Error(`Extraction ${extraction.pattern.source} needs the g and d flags`);
    }
    let masked = '';
    let cursor = 0;
    for (const match of template.matchAll(extraction.pattern)) {
      const name = match.groups?.[extraction.name] ?? extraction.name;
      const spans = Object.entries(extraction.values)
        .map(([group, kind]) => ({ kind, span: match.indices?.groups?.[group], value: match.groups?.[group] }))
        .toSorted((left, right) => (left.span?.[0] ?? 0) - (right.span?.[0] ?? 0));
      for (const { kind, span, value } of spans) {
        if (span === undefined || value === undefined) {
          continue;
        }
        masked += template.slice(cursor, span[0]) + placeholder(kind);
        cursor = span[1];
        values.push({ kind, name, value });
      }
    }
    template = masked + template.slice(cursor);
  }
  return { template, values };
}

export function compareSnapshots(path: string, before: Snapshot, after: Snapshot): FileReview {
  const review = emptyReview();
  if (before.template !== after.template) {
    review.findings.push(`${path}: ${describeDifference(before.template, after.template)}`);
    return review;
  }
  if (before.values.length !== after.values.length) {
    review.findings.push(`${path}: the number of dependency values changed`);
    return review;
  }
  for (const [index, old] of before.values.entries()) {
    const next = after.values[index];
    if (next?.name !== old.name || next.kind !== old.kind) {
      review.findings.push(`${path}: dependency ${old.name} was replaced`);
      continue;
    }
    if (next.value === old.value) {
      continue;
    }
    if (old.kind === 'version') {
      review.updates.push({ after: next.value, before: old.value, name: `${path} ${old.name}` });
    } else if (old.kind === 'fixed') {
      review.findings.push(`${path}: ${old.name} must stay ${old.value}, not ${next.value}`);
    }
  }
  return review;
}

export function describeDifference(before: string, after: string): string {
  const oldLines = before.split('\n');
  const newLines = after.split('\n');
  const index = newLines.findIndex((line, position) => line !== oldLines[position]);
  if (index === -1) {
    const removed = oldLines[newLines.length] ?? '';
    return `content other than dependency values was removed at line ${newLines.length + 1}: ${quote(removed)}`;
  }
  return `content other than dependency values changed at line ${index + 1}: ${quote(newLines[index] ?? '')}`;
}

export function quote(line: string): string {
  return JSON.stringify(line.trim().slice(0, 120));
}

export function mergeReviews(reviews: readonly FileReview[]): FileReview {
  return {
    claims: reviews.flatMap((review) => review.claims),
    findings: reviews.flatMap((review) => review.findings),
    updates: reviews.flatMap((review) => review.updates),
  };
}
