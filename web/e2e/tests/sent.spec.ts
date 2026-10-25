import { expect, test } from './support/fixtures.ts';
import {
  countCompletedWrites,
  databaseNames,
  EXPIRED_RECORD_ID,
  sentRecords,
  storeExpiredRecord,
} from './support/indexed-database.ts';
import { createMessageThroughApi } from './support/messages.ts';
import {
  chooseRemember,
  createdLink,
  createMessageInPage,
  expectMessageShown,
  notInert,
  openApp,
  showMessage,
} from './support/pages.ts';
import { holdRoute } from './support/routes.ts';
import { watchViolations } from './support/watchers.ts';

const REMEMBERED = 'This device remembered the message.';
const NOTHING_REMEMBERED = 'This device remembers no messages.';

function idOf(link: string): string {
  return new URL(link).pathname.slice('/m/'.length);
}

test('the sender cancels a remembered message from the sent list, then forgets every message (D39)', async ({
  page,
  newPage,
}) => {
  const violations = await watchViolations(page);
  const link = await createMessageInPage(page, { text: 'cancel me', remember: true });
  await expect(page.getByText(REMEMBERED)).toBeVisible();
  await page.getByRole('link', { name: 'Status and cancelling' }).click();
  await expect(page.getByRole('heading', { name: 'Sent messages' })).toBeVisible();
  const item = page.getByRole('listitem');
  await expect(item).toHaveCount(1);
  await expect(item.getByText('Waiting to be opened.')).toBeVisible();
  await expect(item.getByText('0 of 1')).toBeVisible();

  const cancel = item.getByRole('button', { name: 'Cancel message' });
  const dialog = notInert(page, page.getByRole('dialog').or(page.getByRole('alertdialog')));
  const focusInItem = () => item.evaluate((element) => element.contains(document.activeElement));
  for (let round = 1; round <= 10; round++) {
    await cancel.click();
    await expect(dialog, `round ${round}`).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect.poll(focusInItem).toBe(true);
    await cancel.focus();
    await page.keyboard.press('Enter');
    await expect(dialog, `round ${round}`).toBeVisible();
    await dialog.getByRole('button', { name: 'Keep it', exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(cancel).toBeFocused();
  }
  await cancel.click();
  await dialog.getByRole('button', { name: 'Yes, cancel', exact: true }).click();
  await expect(item.getByText('Cancelled.')).toBeVisible();
  await expect(cancel).toBeDisabled();
  await expect(item.getByRole('heading', { level: 3 })).toBeFocused();
  await expect(page.getByRole('status').first()).toHaveText('The message is cancelled.');
  await expect.poll(() => sentRecords(page)).toMatchObject([{ id: idOf(link), endedAt: expect.any(Number) }]);

  const recipient = await newPage();
  await recipient.goto(link);
  await expect(recipient.getByText('The sender cancelled the message')).toBeVisible();

  await page.getByRole('button', { name: 'Forget all' }).click();
  await dialog.getByRole('button', { name: 'Yes, forget all', exact: true }).click();
  await expect(page.getByText(NOTHING_REMEMBERED)).toBeVisible();
  expect(await sentRecords(page)).toEqual([]);
  await page.reload();
  await expect(page.getByText(NOTHING_REMEMBERED)).toBeVisible();
  expect(await violations()).toEqual([]);
});

test('this device remembers a new message as soon as it exists, before the animation ends (D39)', async ({
  newPage,
}) => {
  const page = await newPage({ reducedMotion: 'no-preference' });
  const completedWrites = await countCompletedWrites(page);
  await openApp(page, '/');
  await page.getByLabel('Message text').fill('remember early');
  await chooseRemember(page);
  const created = page.waitForResponse(
    (response) => response.url().endsWith('/api/v1/messages') && response.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Create link' }).click();
  const response = await created;
  expect(response.ok()).toBe(true);
  await expect.poll(completedWrites).toBeGreaterThan(0);
  await expect(notInert(page, page.getByLabel('Link to the message'))).toHaveCount(0);
  await page.goto('/sent');
  await expect(page.getByRole('listitem')).toHaveCount(1);
  await expect(page.getByText('Waiting to be opened.')).toBeVisible();
});

test(
  'the manage key lies in IndexedDB only as a non-extractable Ed25519 signing key (D39)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const link = await createMessageInPage(page, { text: 'key', remember: true });
    await expect(page.getByText(REMEMBERED)).toBeVisible();
    expect(await sentRecords(page)).toEqual([
      {
        id: idOf(link),
        fields: ['createdAt', 'expiresAt', 'id', 'privateKey', 'publicKey'],
        publicKeyLength: 32,
        privateKey: { algorithm: 'Ed25519', extractable: false, usages: ['sign'], exportable: false },
      },
    ]);
  },
);

test('a status that answers after the sender forgot the message does not store it again (D39)', async ({
  page,
  newPage,
}) => {
  const completedWrites = await countCompletedWrites(page);
  const link = await createMessageInPage(page, { text: 'forget me', remember: true });
  await expect(page.getByText(REMEMBERED)).toBeVisible();
  const recipient = await newPage();
  const acknowledged = recipient.waitForResponse((response) => response.url().endsWith('/ack'));
  await showMessage(recipient, link);
  await expectMessageShown(recipient);
  const acknowledgement = await acknowledged;
  expect(acknowledgement.status()).toBe(204);

  const status = await holdRoute(page, '**/api/v1/messages/*/status');
  await page.goto('/sent');
  await status.requested;
  await page.getByRole('listitem').getByRole('button', { name: 'Forget', exact: true }).click();
  await expect(page.getByText(NOTHING_REMEMBERED)).toBeVisible();
  expect(await sentRecords(page)).toEqual([]);
  const writesBeforeAnswer = await completedWrites();
  const answered = page.waitForResponse((response) => response.url().endsWith('/status'));
  status.release();
  const response = await answered;
  expect(response.status()).toBe(200);
  await expect.poll(completedWrites).toBeGreaterThan(writesBeforeAnswer);
  expect(await sentRecords(page)).toEqual([]);
  await page.unrouteAll();
  await page.reload();
  await expect(page.getByText(NOTHING_REMEMBERED)).toBeVisible();
});

test('a visitor and a sender who keeps the default leave no database, and every page load drops records 7 days after expiry (D39)', async ({
  page,
  newPage,
}) => {
  const message = await createMessageThroughApi();
  await page.goto(message.link);
  await expect(page.getByRole('button', { name: 'Show message' })).toBeVisible();
  await openApp(page, '/');
  await page.getByRole('button', { name: 'Passphrase', exact: true }).click();
  await expect(page.getByRole('switch', { name: /^Cancel from this device/ })).toHaveAttribute('aria-checked', 'false');
  await page.getByLabel('Message text').fill('not remembered');
  await page.getByRole('button', { name: 'Create link' }).click();
  await createdLink(page);
  await expect(page.getByText(REMEMBERED)).toHaveCount(0);
  expect(await databaseNames(page)).toEqual([]);

  const sender = await newPage();
  const kept = idOf(await createMessageInPage(sender, { text: 'kept', remember: true }));
  await expect(sender.getByText(REMEMBERED)).toBeVisible();
  await storeExpiredRecord(sender);
  const storedIds = async () => {
    const records = await sentRecords(sender);
    return new Set(records.map(({ id }) => id));
  };
  expect(await storedIds()).toEqual(new Set([EXPIRED_RECORD_ID, kept]));
  await sender.goto('/report');
  await expect.poll(storedIds).toEqual(new Set([kept]));
});

test('a browser without IndexedDB still creates the link and says it cannot remember it (D39)', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(globalThis, 'indexedDB', { value: undefined });
  });
  await createMessageInPage(page, { text: 'no storage', remember: true });
  await expect(
    page.getByText('This device could not remember the message, so you cannot cancel it from here.'),
  ).toBeVisible();
  await page.goto('/sent');
  await expect(
    page.getByText('This browser does not let the page store data, so it cannot remember sent messages.'),
  ).toBeVisible();
});
