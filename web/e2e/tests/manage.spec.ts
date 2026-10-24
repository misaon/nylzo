import type { Browser, Page } from '@playwright/test';

import { BASE_URL, createViaApi, createViaUi, expect, expectRevealed, live, openViaUi, test } from './helpers.ts';
import { recordViolations } from './violations.ts';

declare global {
  interface Window {
    nylzoMarker?: number;
    nylzoWrites?: number;
  }
}

async function newPage(browser: Browser): Promise<Page> {
  return (await browser.newContext()).newPage();
}

async function countWrites(page: Page): Promise<void> {
  await page.addInitScript(() => {
    window.nylzoWrites = 0;
    const transaction = IDBDatabase.prototype.transaction;
    IDBDatabase.prototype.transaction = function (this: IDBDatabase, ...args: Parameters<IDBDatabase['transaction']>) {
      const opened = transaction.apply(this, args);
      if (opened.mode === 'readwrite') {
        opened.addEventListener('complete', () => {
          window.nylzoWrites = (window.nylzoWrites ?? 0) + 1;
        });
      }
      return opened;
    };
  });
}

function storedRecords(page: Page): Promise<{ readonly id: string; readonly endedAt?: number }[]> {
  return page.evaluate(
    () =>
      new Promise((resolve, reject) => {
        const opening = indexedDB.open('nylzo');
        opening.onerror = () => reject(opening.error);
        opening.onsuccess = () => {
          const all = opening.result.transaction('sent').objectStore('sent').getAll();
          all.onerror = () => reject(all.error);
          all.onsuccess = () => {
            resolve((all.result as { id: string; endedAt?: number }[]).map(({ id, endedAt }) => ({ id, ...(endedAt === undefined ? {} : { endedAt }) })));
            opening.result.close();
          };
        };
      }),
  );
}

async function createdAndOpened(browser: Browser, sender: Page, text: string): Promise<string> {
  const link = await createViaUi(sender, { text });
  await expect(sender.getByText('This device remembered the message.')).toBeVisible();
  const recipient = await newPage(browser);
  const acked = recipient.waitForResponse((response) => response.url().endsWith('/ack'));
  await openViaUi(recipient, link);
  await expectRevealed(recipient);
  expect((await acked).status()).toBe(204);
  return new URL(link).pathname.slice(3);
}

async function confirm(page: Page, answer: string): Promise<void> {
  const dialog = live(page, page.getByRole('dialog').or(page.getByRole('alertdialog')));
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: answer, exact: true }).click();
  await expect(dialog).toBeHidden();
}

test('the sender sees the status of a remembered message and cancels it (D39)', async ({ browser }) => {
  const sender = await newPage(browser);
  const violations = await recordViolations(sender);
  const link = await createViaUi(sender, { text: 'cancel me' });
  await expect(sender.getByText('This device remembered the message.')).toBeVisible();
  await sender.getByRole('link', { name: 'Status and cancelling' }).click();
  await expect(sender.getByRole('heading', { name: 'Sent messages' })).toBeVisible();
  const item = sender.getByRole('listitem');
  await expect(item).toHaveCount(1);
  await expect(item.getByText('Waiting to be opened.')).toBeVisible();
  await expect(item.getByText('0 of 1')).toBeVisible();

  const cancel = item.getByRole('button', { name: 'Cancel message' });
  const focusInItem = () => item.evaluate((element) => element.contains(document.activeElement));
  await cancel.click();
  await confirm(sender, 'Keep it');
  await expect.poll(focusInItem).toBe(true);
  await cancel.click();
  await sender.keyboard.press('Escape');
  await expect(sender.getByRole('dialog')).toBeHidden();
  await expect.poll(focusInItem).toBe(true);
  await cancel.focus();
  await expect(cancel).toBeFocused();
  await sender.keyboard.press('Enter');
  await confirm(sender, 'Keep it');
  await expect(cancel).toBeFocused();
  await cancel.click();
  await expect(sender.getByRole('dialog')).toBeVisible();
  await confirm(sender, 'Yes, cancel');
  await expect(item.getByText('Cancelled.')).toBeVisible();
  await expect(cancel).toBeDisabled();
  await expect(item.getByRole('heading', { level: 3 })).toBeFocused();
  await expect(sender.getByRole('status').first()).toHaveText('The message is cancelled.');
  await expect(item.getByText('no longer kept')).toHaveCount(2);
  await expect(item.getByText('not yet')).toBeVisible();

  const recipient = await newPage(browser);
  await recipient.goto(link);
  await expect(recipient.getByText('The sender cancelled the message')).toBeVisible();
  expect(await violations()).toEqual([]);
});

test('this device remembers a new message as soon as it exists, before the animation ends (D39)', async ({ browser }) => {
  const sender = await (await browser.newContext({ reducedMotion: 'no-preference' })).newPage();
  await countWrites(sender);
  await sender.goto('/');
  await sender.getByLabel('Message text').fill('remember early');
  const created = sender.waitForResponse((response) => response.url().endsWith('/api/v1/messages') && response.request().method() === 'POST');
  await sender.getByRole('button', { name: 'Create link' }).click();
  expect((await created).ok()).toBe(true);
  await expect.poll(() => sender.evaluate(() => window.nylzoWrites ?? 0)).toBeGreaterThan(0);
  await expect(live(sender, sender.getByLabel('Link to the message'))).toHaveCount(0);
  await sender.goto('/sent');
  await expect(sender.getByRole('listitem')).toHaveCount(1);
  await expect(sender.getByText('Waiting to be opened.')).toBeVisible();
});

test('a dialog closed with Escape opens again on the very next Enter', async ({ browser }) => {
  const sender = await newPage(browser);
  await createViaUi(sender, { text: 'dialog race' });
  await sender.goto('/sent');
  const item = sender.getByRole('listitem');
  await expect(item.getByText('Waiting to be opened.')).toBeVisible();
  const cancel = item.getByRole('button', { name: 'Cancel message' });
  for (let round = 0; round < 10; round++) {
    await cancel.click();
    await expect(sender.getByRole('dialog')).toBeVisible();
    await sender.keyboard.press('Escape');
    await expect(sender.getByRole('dialog')).toBeHidden();
    await cancel.focus();
    await sender.keyboard.press('Enter');
    await expect(sender.getByRole('dialog'), `round ${round}`).toBeVisible();
    await confirm(sender, 'Keep it');
  }
});

test('the manage key lies in IndexedDB only as a non-extractable key (D39)', async ({ browser }) => {
  const sender = await newPage(browser);
  const link = await createViaUi(sender, { text: 'key' });
  const id = new URL(link).pathname.slice(3);
  const stored = await sender.evaluate(
    () =>
      new Promise<unknown[]>((resolve, reject) => {
        const opening = indexedDB.open('nylzo');
        opening.onerror = () => reject(opening.error);
        opening.onsuccess = () => {
          const all = opening.result.transaction('sent').objectStore('sent').getAll();
          all.onerror = () => reject(all.error);
          all.onsuccess = async () => {
            const described = [];
            for (const record of all.result as { privateKey: CryptoKey; publicKey: Uint8Array }[]) {
              const exported = await crypto.subtle.exportKey('pkcs8', record.privateKey).then(
                () => 'exported',
                () => 'refused',
              );
              described.push({
                keys: Object.keys(record).sort(),
                id: (record as unknown as { id: string }).id,
                extractable: record.privateKey.extractable,
                algorithm: record.privateKey.algorithm.name,
                usages: record.privateKey.usages,
                publicKey: record.publicKey.length,
                exported,
              });
            }
            resolve(described);
          };
        };
      }),
  );
  expect(stored).toEqual([
    {
      keys: ['createdAt', 'expiresAt', 'id', 'privateKey', 'publicKey'],
      id,
      extractable: false,
      algorithm: 'Ed25519',
      usages: ['sign'],
      publicKey: 32,
      exported: 'refused',
    },
  ]);
});

test('the sent list stores when a remembered message ended', async ({ browser }) => {
  const sender = await newPage(browser);
  const id = await createdAndOpened(browser, sender, 'ends');
  expect(await storedRecords(sender)).toEqual([{ id }]);
  await sender.goto('/sent');
  await expect(sender.getByRole('listitem').getByText('Opened.')).toBeVisible();
  await expect.poll(() => storedRecords(sender)).toEqual([{ id, endedAt: expect.any(Number) }]);
});

test('a status that answers after the sender forgot the message does not store it again (D39)', async ({ browser }) => {
  const sender = await newPage(browser);
  await countWrites(sender);
  await createdAndOpened(browser, sender, 'forget me');
  const asked = Promise.withResolvers<void>();
  const answer = Promise.withResolvers<void>();
  await sender.route('**/api/v1/messages/*/status', async (route) => {
    asked.resolve();
    await answer.promise;
    await route.continue();
  });
  await sender.goto('/sent');
  await asked.promise;
  await sender.getByRole('listitem').getByRole('button', { name: 'Forget', exact: true }).click();
  await expect(sender.getByText('This device remembers no messages.')).toBeVisible();
  expect(await storedRecords(sender)).toEqual([]);
  const writes = await sender.evaluate(() => window.nylzoWrites ?? 0);
  const answered = sender.waitForResponse((response) => response.url().endsWith('/status'));
  answer.resolve();
  expect((await answered).status()).toBe(200);
  await sender.waitForFunction((before) => (window.nylzoWrites ?? 0) > before, writes);
  expect(await storedRecords(sender)).toEqual([]);
  await sender.unrouteAll();
  await sender.reload();
  await expect(sender.getByText('This device remembers no messages.')).toBeVisible();
});

test('an unchecked box keeps the message off the sent list', async ({ browser }) => {
  const sender = await newPage(browser);
  await sender.goto('/');
  await sender.getByRole('button', { name: 'Passphrase', exact: true }).click();
  const remember = sender.getByRole('switch', { name: /^Cancel from this device/ });
  await remember.click();
  await expect(remember).toHaveAttribute('aria-checked', 'false');
  await sender.getByLabel('Message text').fill('forget me');
  await sender.getByRole('button', { name: 'Create link' }).click();
  await expect(sender.getByLabel('Link to the message')).toBeVisible({ timeout: 45_000 });
  await expect(sender.getByText('This device remembered the message.')).toHaveCount(0);
  await sender.goto('/sent');
  await expect(sender.getByText('This device remembers no messages.')).toBeVisible();
});

test('the sender forgets one message, then all of them, for good', async ({ browser }) => {
  const sender = await newPage(browser);
  for (const text of ['one', 'two', 'three']) {
    await createViaUi(sender, { text });
  }
  await sender.goto('/sent');
  const items = sender.getByRole('listitem');
  await expect(items).toHaveCount(3);
  await expect(items.getByText('Waiting to be opened.')).toHaveCount(3);
  const ids = () => sender.evaluate(() => [...document.querySelectorAll('li h3')].map((heading) => heading.id));
  const before = await ids();
  await items.first().getByRole('button', { name: 'Forget' }).click();
  await confirm(sender, 'Yes, forget');
  await expect(items).toHaveCount(2);
  expect(await ids()).toEqual(before.slice(1));
  await expect(sender.getByRole('heading', { name: 'Sent messages' })).toBeFocused();
  await sender.reload();
  await expect(items.getByText('Waiting to be opened.')).toHaveCount(2);
  expect(await ids()).toEqual(before.slice(1));
  await sender.getByRole('button', { name: 'Forget all' }).click();
  await confirm(sender, 'Yes, forget all');
  await expect(sender.getByText('This device remembers no messages.')).toBeVisible();
  await sender.reload();
  await expect(sender.getByText('This device remembers no messages.')).toBeVisible();
});

test('the recipient deletes a message without showing it (D40)', async ({ browser }) => {
  const message = await createViaApi({ text: 'never shown' });
  const recipient = await newPage(browser);
  const violations = await recordViolations(recipient);
  await recipient.goto(message.link);
  await recipient.getByRole('button', { name: 'Delete without showing' }).click();
  await confirm(recipient, 'Do not delete');
  await expect(recipient.getByRole('button', { name: 'Show message' })).toBeVisible();
  await recipient.getByRole('button', { name: 'Delete without showing' }).click();
  await confirm(recipient, 'Yes, delete');
  await expect(recipient.getByText('You deleted the message')).toBeVisible();
  expect(await violations()).toEqual([]);

  const late = await newPage(browser);
  await late.goto(message.link);
  await expect(late.getByText('The message was deleted')).toBeVisible();
});

test('the reveal waits while a deletion runs', async ({ browser }) => {
  const message = await createViaApi({ text: 'slow delete' });
  const recipient = await newPage(browser);
  let release: () => void = () => undefined;
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  await recipient.route('**/api/v1/messages/*/burn', async (route) => {
    await held;
    await route.continue();
  });
  await recipient.goto(message.link);
  await recipient.getByRole('button', { name: 'Delete without showing' }).click();
  await confirm(recipient, 'Yes, delete');
  await expect(recipient.getByRole('button', { name: 'Show message' })).toBeDisabled();
  await expect(recipient.getByRole('button', { name: 'Delete without showing' })).toBeDisabled();
  release();
  await expect(recipient.getByText('You deleted the message')).toBeVisible();
  await expect(recipient.getByRole('heading', { name: 'One-time message' })).toBeFocused();
});

test('the recipient deletes the remaining opens and keeps reading (D40)', async ({ browser }) => {
  const message = await createViaApi({ text: 'three opens', opensMax: 3 });
  const recipient = await newPage(browser);
  await openViaUi(recipient, message.link);
  await expectRevealed(recipient);
  await recipient.getByRole('button', { name: 'Delete the remaining opens' }).click();
  await confirm(recipient, 'Yes, delete');
  await expect(recipient.getByRole('status').first()).toHaveText('The remaining opens are deleted.');
  await expect(recipient.locator('pre')).toHaveText('three opens');
  await recipient.getByRole('button', { name: 'Čeština' }).click();
  await expect(recipient.getByRole('status').first()).toHaveText('Zbývající otevření jsou smazaná.');
  await recipient.getByRole('button', { name: 'English' }).click();
  await expect(recipient.getByRole('button', { name: 'Delete the remaining opens' })).toBeDisabled();

  const late = await newPage(browser);
  await late.goto(message.link);
  await expect(late.getByText('The message was deleted')).toBeVisible();
});

test('the last open offers no deletion', async ({ browser }) => {
  const message = await createViaApi({ text: 'one open' });
  const recipient = await newPage(browser);
  await openViaUi(recipient, message.link);
  await expectRevealed(recipient);
  await expect(live(recipient, recipient.getByRole('button', { name: 'Delete the remaining opens' }))).toHaveCount(0);
  await expect(live(recipient, recipient.getByRole('button', { name: 'Report', exact: true }))).toHaveCount(0);
});

test('a report from the read page or from /report spends no open (D41)', async ({ browser }) => {
  const message = await createViaApi({ text: 'reported', opensMax: 2 });
  const recipient = await newPage(browser);
  const violations = await recordViolations(recipient);
  await recipient.goto(message.link);
  await recipient.getByRole('button', { name: 'Report', exact: true }).click();
  await recipient.getByLabel('Reason, optional').fill('looks like phishing');
  await recipient.getByRole('button', { name: 'Send report' }).click();
  await expect(recipient.getByRole('status').first()).toHaveText('Report sent');
  await expect(recipient.getByLabel('Reason, optional')).toHaveValue('');

  const reporter = await newPage(browser);
  await reporter.goto('/report');
  await reporter.getByLabel('Link to the message').fill(message.link.replace(BASE_URL, 'http://evil.test'));
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByText('The link belongs to another service.')).toBeVisible();
  await expect(reporter.getByLabel('Link to the message')).toBeFocused();
  await reporter.getByLabel('Link to the message').fill(`  ${message.link}  `);
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByText('We received the report.')).toBeVisible();

  await recipient.getByRole('button', { name: 'Show message' }).click();
  await expectRevealed(recipient);
  await expect(recipient.locator('pre')).toHaveText('reported');
  await recipient.getByRole('button', { name: 'Report', exact: true }).click();
  await recipient.getByLabel('Reason, optional').fill('after reading');
  await recipient.getByRole('button', { name: 'Send report' }).click();
  await expect(recipient.getByRole('status').first()).toHaveText('Report sent');
  expect(await violations()).toEqual([]);
});

test('the language switches in place, keeps a shown message and stays chosen', async ({ browser }) => {
  const message = await createViaApi({ text: 'zůstane' });
  const recipient = await newPage(browser);
  await openViaUi(recipient, message.link);
  await expectRevealed(recipient);
  await recipient.evaluate(() => {
    window.nylzoMarker = 1;
  });
  await recipient.getByRole('button', { name: 'Čeština' }).click();
  await expect(recipient.getByRole('heading', { name: 'Obsah zprávy' })).toBeVisible();
  await expect(recipient.locator('pre')).toHaveText('zůstane');
  expect(await recipient.evaluate(() => [window.nylzoMarker, document.documentElement.lang])).toEqual([1, 'cs']);
  await expect(recipient.getByRole('button', { name: 'English' })).toHaveAttribute('lang', 'en');

  await recipient.goto('/');
  await expect(recipient).toHaveURL(`${BASE_URL}/cs/`);
  await expect(recipient.getByRole('heading', { name: 'Nová zpráva' })).toBeVisible();
  expect(await recipient.evaluate(() => document.documentElement.lang)).toBe('cs');
  await recipient.getByRole('button', { name: 'English' }).click();
  await expect(recipient.getByRole('heading', { name: 'New message' })).toBeVisible();
  await expect(recipient).toHaveURL(`${BASE_URL}/`);
});

test('a failed status can be loaded again (D39)', async ({ browser }) => {
  const sender = await newPage(browser);
  await createViaUi(sender, { text: 'retry' });
  let failures = 1;
  await sender.route('**/api/v1/messages/*/status', (route) =>
    failures-- > 0 ? route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"internal"}' }) : route.continue(),
  );
  await sender.goto('/sent');
  const item = sender.getByRole('listitem');
  await expect(item.getByText('The status could not be loaded.')).toBeVisible();
  await item.getByRole('button', { name: 'Try again' }).click();
  await expect(item.getByText('Waiting to be opened.')).toBeVisible();
  await expect(item.getByRole('heading', { level: 3 })).toBeFocused();
});

test('a browser without IndexedDB still gets the link and says it cannot remember it (D39)', async ({ browser }) => {
  const sender = await newPage(browser);
  await sender.addInitScript(() => {
    Object.defineProperty(window, 'indexedDB', { value: undefined });
  });
  await createViaUi(sender, { text: 'no storage' });
  await expect(sender.getByText('This device could not remember the message, so you cannot cancel it from here.')).toBeVisible();
  await sender.goto('/sent');
  await expect(sender.getByText('This browser does not let the page store data, so it cannot remember sent messages.')).toBeVisible();
});

test('every page load drops records 7 days after expiry and creates no database for visitors (D39)', async ({ browser }) => {
  const message = await createViaApi({ text: 'visitor' });
  const visitor = await newPage(browser);
  await visitor.goto(message.link);
  await expect(visitor.getByRole('button', { name: 'Show message' })).toBeVisible();
  await visitor.goto('/');
  await expect(visitor.getByLabel('Message text')).toBeVisible();
  expect(await visitor.evaluate(async () => (await indexedDB.databases()).map((database) => database.name))).toEqual([]);

  const sender = await newPage(browser);
  const link = await createViaUi(sender, { text: 'kept' });
  const kept = new URL(link).pathname.slice(3);
  await sender.evaluate(async () => {
    const pair = (await crypto.subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify'])) as CryptoKeyPair;
    const publicKey = new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey));
    const old = Date.now() - 8 * 24 * 3_600_000;
    await new Promise<void>((resolve, reject) => {
      const opening = indexedDB.open('nylzo');
      opening.onerror = () => reject(opening.error);
      opening.onsuccess = () => {
        const transaction = opening.result.transaction('sent', 'readwrite');
        transaction.objectStore('sent').put({ id: 'AAAAAAAAAAAAAAAAAAAAAA', createdAt: old - 3_600_000, expiresAt: old, privateKey: pair.privateKey, publicKey });
        transaction.oncomplete = () => resolve();
        transaction.onerror = () => reject(transaction.error);
      };
    });
  });
  const stored = () =>
    sender.evaluate(
      () =>
        new Promise<string[]>((resolve, reject) => {
          const opening = indexedDB.open('nylzo');
          opening.onerror = () => reject(opening.error);
          opening.onsuccess = () => {
            const keys = opening.result.transaction('sent').objectStore('sent').getAllKeys();
            keys.onsuccess = () => resolve((keys.result as string[]).sort());
            keys.onerror = () => reject(keys.error);
          };
        }),
    );
  expect(await stored()).toEqual(['AAAAAAAAAAAAAAAAAAAAAA', kept].sort());
  await sender.goto('/report');
  await expect.poll(stored).toEqual([kept]);
});

