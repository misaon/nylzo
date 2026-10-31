import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';

const releaseKinds = new Set(['manual', 'planned', 'security', 'weekly']);

function required(name: string, pattern: RegExp): string {
  const value = process.env[name];
  if (value === undefined || !pattern.test(value)) {
    throw new Error(`${name} must match ${String(pattern)}`);
  }
  return value;
}

function trailers(message: string): Map<string, string> {
  const values = new Map<string, string>();
  for (const line of message.split('\n')) {
    const separator = line.indexOf(': ');
    const name = line.slice(0, separator);
    if (separator <= 0 || !/^[A-Z][\dA-Za-z-]*$/.test(name)) {
      continue;
    }
    if (values.has(name)) {
      throw new Error(`The tag repeats ${name}`);
    }
    values.set(name, line.slice(separator + 2));
  }
  return values;
}

const output = process.argv[2];
if (output === undefined) {
  throw new Error('Usage: release-policy.ts <output file>');
}
const tag = required('GITHUB_REF_NAME', /^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/);
const source = required('GITHUB_SHA', /^[\da-f]{40}$/);
const claims = trailers(execFileSync('git', ['cat-file', 'tag', `refs/tags/${tag}`], { encoding: 'utf8' }));
const kind = claims.get('Release-Kind') ?? 'manual';
if (!releaseKinds.has(kind)) {
  throw new Error(`Unknown release kind ${kind}`);
}
const policyCommit = claims.get('Policy-Commit') ?? null;
if (kind !== 'manual' && (policyCommit === null || !/^[\da-f]{40}$/.test(policyCommit))) {
  throw new Error('An automated release tag must name its signing workflow commit');
}
writeFileSync(output, `${JSON.stringify({ format: 1, tag, source, kind, policyCommit }, undefined, 2)}\n`);
