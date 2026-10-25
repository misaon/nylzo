import { describe, expect, it } from 'vitest';

import { ApiError } from '../src/lib/api.ts';
import { createMessage, type CreateDeps } from '../src/lib/create.ts';
import { reasonTooLong, redactReason, reportLink } from '../src/lib/report.ts';
import { fakeApi, type FakeApi, type FakeOptions } from './fake-api.ts';

const ORIGIN = 'https://nylzo.test';

async function setup(options: FakeOptions = {}): Promise<{ api: FakeApi; link: string; id: string }> {
  const api = fakeApi(options);
  const deps: CreateDeps = { api, origin: ORIGIN, scrypt: async () => new Uint8Array(32), solvePow: async () => '' };
  const created = await createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: '' }, deps);
  api.calls.length = 0;
  return { api, link: created.link, id: created.id };
}

describe('reporting a pasted link (D41)', () => {
  it('signs the report with the read key derived from the link and never claims the message', async () => {
    const { api, link, id } = await setup();
    expect(await reportLink(api, ORIGIN, `  ${link}\n`, 'phishing')).toEqual({ outcome: 'accepted' });
    expect(api.reports).toEqual([{ id, signedByReader: true, reason: 'phishing' }]);
    expect(api.calls).toEqual(['report']);
  });

  it('accepts the scheme and host in any letter case', async () => {
    const { api, link } = await setup();
    const shouting = link.replace('https://nylzo.test', 'HTTPS://NYLZO.TEST');
    expect(await reportLink(api, ORIGIN, shouting, '')).toEqual({ outcome: 'accepted' });
  });

  it('refuses a link of another service without sending anything', async () => {
    const { api, link } = await setup();
    for (const foreign of [
      link.replace('nylzo.test', 'evil.test'),
      link.replace('https://', 'http://'),
      link.replace('nylzo.test', 'nylzo.test:8443'),
    ]) {
      expect(await reportLink(api, ORIGIN, foreign, '')).toEqual({ outcome: 'invalid', problems: ['link_foreign'] });
    }
    expect(api.calls).toEqual([]);
  });

  it('refuses a damaged link and a long reason together', async () => {
    const { api, link } = await setup();
    expect(await reportLink(api, ORIGIN, link.split('#')[0] ?? '', 'ab '.repeat(334))).toEqual({
      outcome: 'invalid',
      problems: ['link_invalid', 'reason_too_long'],
    });
    expect(await reportLink(api, ORIGIN, `${link}x`, '')).toEqual({ outcome: 'invalid', problems: ['link_invalid'] });
    expect(api.calls).toEqual([]);
  });

  it('tells a link from a newer version apart and sends nothing', async () => {
    const { api, link } = await setup();
    expect(await reportLink(api, ORIGIN, link.replace('#1.', '#2.'), '')).toEqual({
      outcome: 'invalid',
      problems: ['link_newer'],
    });
    expect(await reportLink(api, ORIGIN, link.replace('#1.', '#0.'), '')).toEqual({
      outcome: 'invalid',
      problems: ['link_invalid'],
    });
    expect(api.calls).toEqual([]);
  });

  it('tells a link with the retired passphrase profile apart and sends nothing', async () => {
    const { api, link } = await setup();
    expect(await reportLink(api, ORIGIN, link.replace('#1.0.', '#1.2.'), '')).toEqual({
      outcome: 'invalid',
      problems: ['link_retired'],
    });
    expect(await reportLink(api, ORIGIN, link.replace('/m/', '/x/').replace('#1.0.', '#1.2.'), '')).toEqual({
      outcome: 'invalid',
      problems: ['link_invalid'],
    });
    expect(api.calls).toEqual([]);
  });

  it('passes on a rate limit and other failures', async () => {
    const limited = await setup({ failReport: [new ApiError(429, 'rate_limited', 60)] });
    expect(await reportLink(limited.api, ORIGIN, limited.link, '')).toEqual({ outcome: 'retry_later', retryAfter: 60 });
    const broken = await setup({ failReport: [new ApiError(500, 'internal')] });
    expect(await reportLink(broken.api, ORIGIN, broken.link, '')).toEqual({ outcome: 'failed' });
  });
});

describe('the length of a reason', () => {
  it('allows 1000 bytes and refuses more', () => {
    expect(reasonTooLong('a'.repeat(1000))).toBe(false);
    expect(reasonTooLong('a'.repeat(1001))).toBe(true);
    expect(reasonTooLong('č'.repeat(500))).toBe(false);
    expect(reasonTooLong('č'.repeat(501))).toBe(true);
  });

  it('refuses a reason whose JSON body would pass the server limit of 4096 bytes', () => {
    expect(reasonTooLong('"'.repeat(1000))).toBe(false);
    expect(reasonTooLong('\u0001'.repeat(700))).toBe(true);
  });
});

describe('redacting a reason before it is signed (D41)', () => {
  it('removes a pasted link and S in every form the server decodes', async () => {
    const { api, link } = await setup();
    const secret = link.split('.').at(-1) ?? '';
    const zeroWidth = `${secret.slice(0, 20)}\u200b${secret.slice(20)}`;
    for (const reason of [
      `phishing, see ${link}`,
      `see ${encodeURIComponent(link)}`,
      `see ${encodeURIComponent(encodeURIComponent(link))}`,
      `key ${secret}`,
      `key ${zeroWidth}`,
      `key ${[...secret].map((c) => `%${c.charCodeAt(0).toString(16)}`).join('')}`,
    ]) {
      const redacted = redactReason(reason);
      expect(redacted, reason).not.toContain(secret);
      expect(redacted, reason).toContain('[redacted]');
    }
    expect(await reportLink(api, ORIGIN, link, `phishing, see ${link}`)).toEqual({ outcome: 'accepted' });
    expect(api.reports[0]?.reason).toBe('phishing, see https://nylzo.test/m/[redacted][redacted]');
  });

  it('redacts S that was percent-encoded three times, as deep as the server decodes', () => {
    const secret = 'AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-AbCdE';
    const triple = [...secret].map((c) => `%2525${c.charCodeAt(0).toString(16)}`).join('');
    expect(redactReason(`key ${triple}`)).toBe('key [redacted]');
  });

  it('keeps ordinary text and turns control characters into spaces', () => {
    expect(redactReason('phishing "quoted" ✓ 100% sure')).toBe('phishing "quoted" ✓ 100% sure');
    expect(redactReason('line\none\ttab\u0007bell')).toBe('line one tab bell');
    expect(redactReason('bidi\u202eflip')).toBe('bidiflip');
    expect(redactReason('%25%32%35')).toBe('%');
  });

  it('measures the redacted reason against the limit', async () => {
    const { api, link } = await setup();
    const long = `${link.split('.').at(-1) ?? ''} `.repeat(30);
    expect(utf8Length(long)).toBeGreaterThan(1000);
    expect(await reportLink(api, ORIGIN, link, long)).toEqual({ outcome: 'accepted' });
  });
});

function utf8Length(text: string): number {
  return new TextEncoder().encode(text).length;
}

describe('the reason length after redaction, as nylzo report checks it', () => {
  it.each([
    ['', false],
    ['phishing', false],
    ['ab '.repeat(400), true],
    ['a'.repeat(3000), false],
    ['# '.repeat(150), true],
    ['# '.repeat(50), false],
    ['<'.repeat(1000), false],
    ['ab '.repeat(333) + 'ab', true],
  ])('%#', (reason, long) => {
    expect(reasonTooLong(redactReason(reason))).toBe(long);
  });
});
