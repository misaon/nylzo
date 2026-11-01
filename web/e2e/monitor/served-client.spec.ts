import type { Page } from '@playwright/test';

import { createHash, randomBytes } from 'node:crypto';
import { readFileSync } from 'node:fs';

import { expect, test } from '../tests/support/fixtures.ts';
import { createMessageInPage, expectMessageShown, showMessage } from '../tests/support/pages.ts';

const instance = new URL(process.env['NYLZO_MONITOR_URL'] ?? 'https://monitor.invalid/');
const signed = new Map(
  readFileSync(process.env['NYLZO_MONITOR_CHECKSUMS'] ?? '/dev/null', 'utf8')
    .split('\n')
    .map((line) => line.split(/\s+/u))
    .filter((fields): fields is [string, string] => fields.length === 2 && /^[\da-f]{64}$/u.test(fields[0] ?? ''))
    .map(([digest, name]) => [name, digest]),
);

function recordAssets(page: Page, served: Map<string, string>): void {
  page.on('response', async (response) => {
    const url = new URL(response.url());
    if (url.origin === instance.origin && url.pathname.startsWith('/assets/')) {
      served.set(
        url.pathname.slice('/assets/'.length),
        createHash('sha256')
          .update(await response.body())
          .digest('hex'),
      );
    }
  });
}

test('a browser that creates and reads a message receives only the signed release assets', async ({ newPage }) => {
  expect(
    signed.size,
    'NYLZO_MONITOR_CHECKSUMS names the verified checksums.txt of the running release',
  ).toBeGreaterThan(0);
  const served = new Map<string, string>();
  const sender = await newPage();
  recordAssets(sender, served);
  const link = await createMessageInPage(
    sender,
    { text: `Nylzo browser check ${randomBytes(16).toString('hex')}` },
    instance.origin,
  );
  const reader = await newPage();
  recordAssets(reader, served);
  await showMessage(reader, link);
  await expectMessageShown(reader);
  expect(served.size).toBeGreaterThan(0);
  for (const [name, digest] of served) {
    expect(signed.get(name), `served asset ${name}`).toBe(digest);
  }
});
