import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

import { redactReason } from './report.ts';

interface RedactVector {
  readonly name: string;
  readonly reason: string;
  readonly redacted: string;
}

const { redact } = JSON.parse(readFileSync(new URL('../../../../../spec/vectors.json', import.meta.url), 'utf8')) as {
  readonly redact: readonly RedactVector[];
};

describe('redaction matches the server (spec/vectors.json)', () => {
  it('has vectors', () => {
    expect(redact.length).toBeGreaterThan(0);
  });
  it.each(redact)('$name', ({ reason, redacted }) => {
    expect(redactReason(reason)).toBe(redacted);
  });
});
