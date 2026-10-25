import { expect, test } from './support/fixtures.ts';
import { createMessageThroughApi } from './support/messages.ts';
import { expectMessageShown, showMessage } from './support/pages.ts';
import { encodedForms, expectNoSecretSent, recordRequests } from './support/requests.ts';
import { BASE_URL } from './support/servers.ts';
import { watchViolations } from './support/watchers.ts';

test('an abuse report from the read page or from /report spends no open and sends only the id, the reason and a signature (AK2, D41)', async ({
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
  await page.getByRole('button', { name: 'Send report' }).click();
  await expect(page.getByRole('status').first()).toHaveText('Report sent');
  await expect(reason).toHaveValue('');

  const reporter = await newPage();
  const reporterRequests = recordRequests(reporter);
  const link = reporter.getByLabel('Link to the message');
  await reporter.goto('/report');
  await link.fill(message.link.replace(BASE_URL, 'http://evil.test'));
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByText('The link belongs to another service.')).toBeVisible();
  await expect(link).toBeFocused();
  await link.fill(`  ${message.link}  `);
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByText('We received the report.')).toBeVisible();
  expect(await message.status()).toMatchObject({ state: 'available', opensUsed: 0 });

  await page.getByRole('button', { name: 'Show message' }).click();
  await expectMessageShown(page);
  await expect(page.locator('pre')).toHaveText('reported');
  await report.click();
  await reason.fill('after reading');
  await page.getByRole('button', { name: 'Send report' }).click();
  await expect(page.getByRole('status').first()).toHaveText('Report sent');
  expect(await violations()).toEqual([]);

  const requests = [...(await readerRequests()), ...(await reporterRequests())];
  expectNoSecretSent(requests, [
    new URL(message.link).hash.slice(1),
    ...encodedForms(Buffer.from(message.secret, 'base64url')),
  ]);
  const reports = requests.filter((request) => request.url.endsWith('/api/v1/abuse'));
  expect(reports.map((sent) => JSON.parse(sent.body) as unknown)).toEqual([
    { id: message.id, reason: 'looks like phishing' },
    { id: message.id, reason: 'after reading' },
    { id: message.id },
  ]);
  for (const sent of reports) {
    expect(sent.headers['authorization']).toMatch(/^Nylzo-Sig /);
    expect(sent.headers['content-type']).toBe('application/json');
  }
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
    await page.getByRole('button', { name: 'Send report' }).click();
    await expect(page.getByRole('status').first()).toHaveText('Report sent');

    await page.goto('/report');
    await page.getByLabel('Link to the message').fill(message.link);
    await page.getByRole('button', { name: 'Send report' }).click();
    await expect(page.getByText('We received the report.')).toBeVisible();
  },
);
