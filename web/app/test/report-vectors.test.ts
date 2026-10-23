import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

import { redactReason } from '../src/lib/report.ts';

interface RedactVector {
  readonly name: string;
  readonly reason: string;
  readonly redacted: string;
}

const { redact } = JSON.parse(readFileSync(new URL('../../../spec/vectors.json', import.meta.url), 'utf8')) as { readonly redact: readonly RedactVector[] };

describe('redaction matches the server (spec/vectors.json)', () => {
  it('has vectors', () => {
    expect(redact.length).toBeGreaterThan(0);
  });
  for (const v of redact) {
    it(v.name, () => {
      expect(redactReason(v.reason)).toBe(v.redacted);
    });
  }
});
