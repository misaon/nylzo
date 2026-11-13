import { expect, test } from '../support/fixtures.ts';
import {
  countCompletedWrites,
  databaseNames,
  EXPIRED_RECORD_ID,
  sentRecords,
  storeExpiredRecord,
} from '../support/indexed-database.ts';
import { createMessageThroughApi } from '../support/messages.ts';
import {
  chooseLanguage,
  createdLink,
  createMessageInPage,
  expectMessageShown,
  notInert,
  openApp,
  showMessage,
  trackInSent,
} from '../support/pages.ts';
import { holdRoute } from '../support/routes.ts';
import { watchViolations } from '../support/watchers.ts';

const REMEMBERED = 'This device remembered the message.';
const NOTHING_REMEMBERED = 'Nothing here yet';

function idOf(link: string): string {
  return new URL(link).hash.slice(1).split('.', 3)[2] ?? '';
}

test('the sender cancels a remembered message from the sent list, then forgets every message (D39)', async ({
  page,
  newPage,
}) => {
  const violations = await watchViolations(page);
  const link = await createMessageInPage(page, { text: 'cancel me', remember: 'browser' });
  await expect(page.getByText(REMEMBERED)).toBeVisible();
  await page.getByRole('link', { name: 'Open Sent' }).click();
  await expect(page.getByRole('heading', { name: 'Sent', exact: true })).toBeVisible();
  const item = page.getByRole('listitem');
  await expect(item).toHaveCount(1);
  await expect(item.getByText('Waiting to be opened')).toBeVisible();
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
  await expect(item.getByText('Cancelled', { exact: true })).toBeVisible();
  await expect(cancel).toHaveCount(0);
  await expect(item.getByRole('heading', { level: 2 })).toBeFocused();
  await expect(page.getByRole('status').first()).toHaveText('The message is cancelled.');
  await expect(item.getByText('the entry goes when you leave this page')).toBeVisible();
  await expect
    .poll(() => sentRecords(page), 'an ended message leaves storage once the list shows it (F16)')
    .toEqual([]);

  const recipient = await newPage();
  await recipient.goto(link);
  await expect(recipient.getByText('The sender cancelled the message')).toBeVisible();

  await page.getByRole('button', { name: 'Clear the list' }).click();
  await dialog.getByRole('button', { name: 'Yes, clear', exact: true }).click();
  await expect(page.getByText(NOTHING_REMEMBERED)).toBeVisible();
  expect(await sentRecords(page)).toEqual([]);
  await page.reload();
  await expect(page.getByText(NOTHING_REMEMBERED)).toBeVisible();
  expect(await violations()).toEqual([]);
});

test(
  'a message tracked only in this tab stays out of browser storage and moves with the tab between pages (F16)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const completedWrites = await countCompletedWrites(page);
    await createMessageInPage(page, { text: 'only this tab', remember: 'tab' });
    await page.evaluate(() => {
      Reflect.set(globalThis, 'sameDocument', true);
    });
    const sameDocument = () => page.evaluate(() => Reflect.get(globalThis, 'sameDocument') === true);
    await page.getByRole('link', { name: 'Open Sent' }).click();
    await expect(page).toHaveURL(/\/sent$/u);
    const item = page.getByRole('listitem');
    await expect(item.getByText('Waiting to be opened')).toBeVisible();
    await expect(item.getByText(/in this tab only/u).filter({ visible: true })).toBeVisible();
    await page.getByRole('link', { name: 'Transparency' }).first().click();
    await expect(page).toHaveURL(/\/transparency$/u);
    await page.goBack();
    await expect(item.getByText('Waiting to be opened')).toBeVisible();
    expect(await sameDocument()).toBe(true);

    await item.getByRole('button', { name: 'Cancel message' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Yes, cancel', exact: true }).click();
    await expect(item.getByText('Cancelled', { exact: true })).toBeVisible();
    expect(await completedWrites()).toBe(0);
    expect(await databaseNames(page)).toEqual([]);

    await createMessageInPage(page, { text: 'left behind', remember: 'tab' });
    await page.getByRole('link', { name: 'Open Sent' }).click();
    await expect(item.getByText('Waiting to be opened')).toBeVisible();
    await page.evaluate(() => {
      dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }));
    });
    await expect(page.getByRole('listitem')).toHaveCount(0);
    await page.evaluate(() => {
      dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }));
    });
    await expect(
      page.getByText(NOTHING_REMEMBERED),
      'a page kept for the back button forgets the tab list',
    ).toBeVisible();
    expect(await completedWrites()).toBe(0);
  },
);

test('switching the language with motion while the tab tracks a message moves the address to the new home (F16)', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await createMessageInPage(page, { text: 'language', remember: 'tab' });
  await chooseLanguage(page, 'Čeština');
  await expect(page).toHaveURL(/\/cs\/$/u);
  await expect(page.getByText('Tato karta si zprávu pamatuje nejdéle do zavření nebo obnovení.')).toBeVisible();
  await page.evaluate(() => {
    Reflect.set(globalThis, 'sameDocument', true);
  });
  await page.getByRole('link', { name: 'Otevřít Odeslané' }).click();
  await expect(page.getByRole('listitem').getByText('Čeká na otevření')).toBeVisible();
  await page
    .getByRole('link', { name: /Napsat/u })
    .first()
    .click();
  await expect(page).toHaveURL(/\/cs\/$/u);
  await page.goBack();
  await expect(page.getByRole('listitem').getByText('Čeká na otevření')).toBeVisible();
  expect(await page.evaluate(() => Reflect.get(globalThis, 'sameDocument') === true)).toBe(true);
});

test('this device remembers a new message only when the sender saves it in the created panel (UX13)', async ({
  page,
}) => {
  const completedWrites = await countCompletedWrites(page);
  await openApp(page, '/');
  await page.getByLabel('Message text').fill('remember on request');
  await page.getByRole('button', { name: 'Create link' }).click();
  await createdLink(page);
  expect(await completedWrites()).toBe(0);
  expect(await databaseNames(page)).toEqual([]);
  await trackInSent(page, 'Password for Peter');
  await expect(page.getByText(REMEMBERED)).toBeVisible();
  await expect.poll(completedWrites).toBeGreaterThan(0);
  await page.goto('/sent');
  await expect(page.getByRole('listitem')).toHaveCount(1);
  await expect(page.getByText('Waiting to be opened')).toBeVisible();
});

test(
  'the manage key lies in IndexedDB only as a non-extractable Ed25519 signing key (D39)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const link = await createMessageInPage(page, { text: 'key', remember: 'browser' });
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
  const link = await createMessageInPage(page, { text: 'forget me', remember: 'browser' });
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
  await page.getByRole('listitem').getByRole('button', { name: 'Remove from the list' }).click();
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
  expect(await sentRecords(page)).toEqual([]);
});

test('a visitor, a sender who keeps the default and a look at the sent list leave no database, and every page load drops records 7 days after expiry (D39)', async ({
  page,
  newPage,
}) => {
  const message = await createMessageThroughApi();
  await page.goto(message.link);
  await expect(page.getByRole('button', { name: 'Show message' })).toBeVisible();
  await openApp(page, '/');
  await page.getByLabel('Message text').fill('not remembered');
  await page.getByRole('button', { name: 'Create link' }).click();
  await createdLink(page);
  await expect(page.getByText(REMEMBERED)).toHaveCount(0);
  expect(await databaseNames(page)).toEqual([]);
  await page.goto('/sent');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  expect(await databaseNames(page), 'opening the sent list creates no database').toEqual([]);

  const sender = await newPage();
  const kept = idOf(await createMessageInPage(sender, { text: 'kept', remember: 'browser' }));
  await expect(sender.getByText(REMEMBERED)).toBeVisible();
  await storeExpiredRecord(sender);
  const storedIds = async () => {
    const records = await sentRecords(sender);
    return new Set(records.map(({ id }) => id));
  };
  expect(await storedIds()).toEqual(new Set([EXPIRED_RECORD_ID, kept]));
  await sender.goto('/terms');
  await expect.poll(storedIds).toEqual(new Set([kept]));
});

test('a browser without IndexedDB still creates the link and says it cannot remember it (D39)', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(globalThis, 'indexedDB', { value: undefined });
  });
  await createMessageInPage(page, { text: 'no storage', remember: 'browser' });
  await expect(
    page.getByText('This device could not remember the message, so you cannot cancel it from here.'),
  ).toBeVisible();
  await page.goto('/sent');
  await expect(
    page.getByText('This browser does not let the page store data, so it cannot remember sent messages.'),
  ).toBeVisible();
});
