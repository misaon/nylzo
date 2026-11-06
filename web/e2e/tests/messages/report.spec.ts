import type { Page } from '@playwright/test';

import { expect, test } from '../support/fixtures.ts';
import { createMessageThroughApi } from '../support/messages.ts';
import { expectMessageShown, showMessage } from '../support/pages.ts';
import { encodedForms, expectNoSecretSent, recordRequests } from '../support/requests.ts';
import { BASE_URL } from '../support/servers.ts';
import { watchViolations } from '../support/watchers.ts';

function goodFaith(page: Page) {
  return page.getByRole('checkbox', { name: /^I believe in good faith/u });
}

test('an abuse report from /report spends no open and sends only the id, the contact, the statement and a signature (AK2, D41)', async ({
  page,
  newPage,
}) => {
  const message = await createMessageThroughApi({ text: 'reported', opensMax: 2 });
  const readerRequests = recordRequests(page);
  const violations = await watchViolations(page);
  await page.goto(message.link);

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
  expect(await violations()).toEqual([]);

  await link.fill(message.link);
  await goodFaith(reporter).check();
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByText('We received the report.')).toBeVisible();

  const requests = [...(await readerRequests()), ...(await reporterRequests())];
  expectNoSecretSent(requests, [
    new URL(message.link).hash.slice(1),
    ...encodedForms(Buffer.from(message.secret, 'base64url')),
  ]);
  const reports = requests.filter((request) => request.url.endsWith('/api/v1/abuse'));
  expect(reports.map((sent) => JSON.parse(sent.body) as unknown)).toEqual([
    { id: message.id, contact: 'jana@example.cz', good_faith: true },
    { id: message.id, good_faith: true },
  ]);
  for (const sent of reports) {
    expect(sent.headers['authorization']).toMatch(/^Nylzo-Sig /u);
    expect(sent.headers['content-type']).toBe('application/json');
  }
});

test(
  'a link whose read key starts with a zero byte opens and can be reported from /report (D66)',
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

    await page.goto('/report');
    await page.getByLabel('Link to the message').fill(message.link);
    await goodFaith(page).check();
    await page.getByRole('button', { name: 'Send report' }).click();
    await expect(page.getByText('We received the report.')).toBeVisible();
  },
);
