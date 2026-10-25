import type { Page } from '@playwright/test';

import { expect, test } from './support/fixtures.ts';
import { createMessageThroughApi } from './support/messages.ts';
import { expectMessageShown, showMessage } from './support/pages.ts';
import { encodedForms, expectNoSecretSent, recordRequests } from './support/requests.ts';
import { BASE_URL } from './support/servers.ts';
import { watchViolations } from './support/watchers.ts';

function goodFaith(page: Page) {
  return page.getByRole('checkbox', { name: /^I believe in good faith/ });
}

test('an abuse report from the read page or from /report spends no open and sends only the id, the reason, the contact, the statement and a signature (AK2, D41)', async ({
  page,
  newPage,
}) => {
  const message = await createMessageThroughApi({ text: 'reported', opensMax: 2 });
  const readerRequests = recordRequests(page);
  const violations = await watchViolations(page);
  const report = page.getByRole('button', { name: 'Report', exact: true });
  const reason = page.getByLabel('Reason, optional');
  await page.goto(message.link);
  await report.click();
  await reason.fill('looks like phishing');
  await page.getByLabel('Email, optional').fill('reader@example.cz');
  await goodFaith(page).check();
  await page.getByRole('button', { name: 'Send report' }).click();
  await expect(page.getByRole('status').first()).toHaveText('Report sent');
  await expect(reason).toHaveValue('');

  const reporter = await newPage();
  const reporterRequests = recordRequests(reporter);
  const link = reporter.getByLabel('Link to the message');
  const contact = reporter.getByLabel('Email, optional');
  await reporter.goto('/report');
  await link.fill(message.link.replace(BASE_URL, 'http://evil.test'));
  await contact.fill('jana@example');
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByText('The link belongs to another service.')).toBeVisible();
  await expect(reporter.getByText('This does not look like an email address.')).toBeVisible();
  await expect(reporter.getByText('Confirm this statement to send the report.')).toBeVisible();
  await expect(link).toBeFocused();
  await link.fill(`  ${message.link}  `);
  await contact.fill(' jana@example.cz ');
  await goodFaith(reporter).check();
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByText('We received the report.')).toBeVisible();
  expect(await message.status()).toMatchObject({ state: 'available', opensUsed: 0 });

  await page.getByRole('button', { name: 'Show message' }).click();
  await expectMessageShown(page);
  await expect(page.locator('pre')).toHaveText('reported');
  await report.click();
  await reason.fill('after reading');
  await goodFaith(page).check();
  await page.getByRole('button', { name: 'Send report' }).click();
  await expect(page.getByRole('status').first()).toHaveText('Report sent');
  expect(await violations()).toEqual([]);

  await link.fill(message.link);
  await goodFaith(reporter).check();
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByRole('status').filter({ hasText: 'We did not store this report.' })).toBeVisible();

  const requests = [...(await readerRequests()), ...(await reporterRequests())];
  expectNoSecretSent(requests, [
    new URL(message.link).hash.slice(1),
    ...encodedForms(Buffer.from(message.secret, 'base64url')),
  ]);
  const reports = requests.filter((request) => request.url.endsWith('/api/v1/abuse'));
  expect(reports.map((sent) => JSON.parse(sent.body) as unknown)).toEqual([
    { id: message.id, reason: 'looks like phishing', contact: 'reader@example.cz', good_faith: true },
    { id: message.id, reason: 'after reading', good_faith: true },
    { id: message.id, contact: 'jana@example.cz', good_faith: true },
    { id: message.id, good_faith: true },
  ]);
  for (const sent of reports) {
    expect(sent.headers['authorization']).toMatch(/^Nylzo-Sig /);
    expect(sent.headers['content-type']).toBe('application/json');
  }

  const late = await newPage();
  await late.goto(message.link);
  await late.getByRole('button', { name: 'Report', exact: true }).click();
  await goodFaith(late).check();
  await late.getByRole('button', { name: 'Send report' }).click();
  await expect(late.getByRole('status').first()).toHaveText('Report not stored');
  await expect(late.getByText('The message already has three reports today')).toBeVisible();
});

test(
  'a link whose read key starts with a zero byte opens and can be reported from the read page and from /report (D66)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createMessageThroughApi({
      text: 'nula na začátku',
      opensMax: 2,
      readKeyStartsWithZero: true,
    });
    await showMessage(page, message.link);
    await expectMessageShown(page);
    await expect(page.locator('pre')).toHaveText('nula na začátku');
    await page.getByRole('button', { name: 'Report', exact: true }).click();
    await page.getByLabel('Reason, optional').fill('zero byte');
    await goodFaith(page).check();
    await page.getByRole('button', { name: 'Send report' }).click();
    await expect(page.getByRole('status').first()).toHaveText('Report sent');

    await page.goto('/report');
    await page.getByLabel('Link to the message').fill(message.link);
    await goodFaith(page).check();
    await page.getByRole('button', { name: 'Send report' }).click();
    await expect(page.getByText('We received the report.')).toBeVisible();
  },
);
