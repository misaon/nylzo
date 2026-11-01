import { type BrowserContext, chromium, type Page } from '@playwright/test';
import { mkdtempSync, readdirSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

import { expect, test } from '../support/fixtures.ts';
import { createMessageThroughApi } from '../support/messages.ts';
import { expectMessageShown, openApp, showMessage } from '../support/pages.ts';
import { BASE_URL } from '../support/servers.ts';

const PASSPHRASE = 'four random words here';

function filesUnder(directory: string): string[] {
  return readdirSync(directory, { recursive: true, withFileTypes: true })
    .filter((entry) => entry.isFile())
    .map((entry) => path.join(entry.parentPath, entry.name));
}

function filesHolding(directory: string, needle: string): string[] {
  const forms = [Buffer.from(needle, 'utf8'), Buffer.from(needle, 'utf16le')];
  return filesUnder(directory)
    .filter((file) => {
      const bytes = readFileSync(file);
      return forms.some((form) => bytes.includes(form));
    })
    .map((file) => path.relative(directory, file));
}

async function withChromiumProfile(use: (context: BrowserContext, profile: string) => Promise<void>): Promise<void> {
  const profile = mkdtempSync(path.join(tmpdir(), 'nylzo-profile-'));
  try {
    const context = await chromium.launchPersistentContext(profile, {
      channel: 'chromium',
      locale: 'en-US',
      reducedMotion: 'reduce',
      permissions: ['local-network-access'],
    });
    try {
      await use(context, profile);
    } finally {
      await context.close();
    }
  } finally {
    rmSync(profile, { recursive: true, force: true });
  }
}

async function browserStorage(page: Page) {
  return page.evaluate(async () => {
    const databases = await indexedDB.databases();
    return {
      session: Object.fromEntries(Object.keys(sessionStorage).map((key) => [key, sessionStorage.getItem(key)])),
      databases: databases.map(({ name = '' }) => name),
      caches: await caches.keys(),
    };
  });
}

async function databaseNames(page: Page): Promise<string[]> {
  const stored = await browserStorage(page);
  return stored.databases;
}

test(
  'an interrupted open keeps only the lease, and opening the link again finishes it without another open (W2-01)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createMessageThroughApi({ text: 'finish from the lease' });
    await page.route('**/api/v1/messages/*/claim', async (route) => {
      const response = await route.fetch();
      await route.fulfill({ response, body: Buffer.alloc(0) });
    });
    await showMessage(page, message.link);
    await expect(page.getByText('The message cannot be opened safely')).toBeVisible();
    const stored = await browserStorage(page);
    expect(Object.keys(stored.session).toSorted((left, right) => left.localeCompare(right))).toEqual([
      'nylzo:dropped:r',
      'nylzo:lease:r',
    ]);
    expect(JSON.stringify(stored)).not.toContain(message.secret);
    expect(stored.databases).toEqual([]);
    expect(stored.caches).toEqual([]);

    await page.unrouteAll();
    await page.reload();
    await expect(page.getByText('Open the link again to finish')).toBeVisible();
    await page.getByLabel('Link to open').fill(message.link);
    await page.getByRole('button', { name: 'Open', exact: true }).click();
    await expect(page.getByText('You already opened the message on this page')).toBeVisible();
    await page.getByRole('button', { name: 'Show message' }).click();
    await expectMessageShown(page);
    await expect(page.locator('pre')).toHaveText('finish from the lease');
    expect(await message.status()).toMatchObject({ state: 'opened' });
  },
);

test(
  'deletes the encrypted link copies that earlier clients stored (W2-01)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const id = 'AAAAAAAAAAAAAAAAAAAAAA';
    await openApp(page, '/sent');
    await page.evaluate(async (messageId) => {
      await new Promise<void>((resolve, reject) => {
        const opening = indexedDB.open('nylzo-resume', 1);
        opening.addEventListener('upgradeneeded', () => {
          opening.result.createObjectStore('keys', { keyPath: 'token' });
        });
        opening.addEventListener('success', () => {
          opening.result.close();
          resolve();
        });
        opening.addEventListener('error', () => {
          reject(opening.error ?? new Error('the old store did not open'));
        });
      });
      sessionStorage.setItem(`nylzo:link:${messageId}`, 'a'.repeat(43));
      sessionStorage.setItem(`nylzo:lease:${messageId}`, 'lease');
    }, id);
    await openApp(page, '/sent');
    await expect.poll(async () => databaseNames(page)).not.toContain('nylzo-resume');
    const stored = await browserStorage(page);
    expect(stored.session).toEqual({ [`nylzo:lease:${id}`]: 'lease' });
  },
);

test(
  'the read page takes the key out of the address bar even when its scripts never arrive (W2-04)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createMessageThroughApi();
    const held: string[] = [];
    await page.route('**/assets/*.js', (route) => {
      held.push(new URL(route.request().url()).pathname);
    });
    await page.goto(message.link, { waitUntil: 'commit' });
    await expect.poll(() => held.length).toBeGreaterThan(0);
    await expect.poll(() => page.evaluate(() => location.hash)).toBe('');
    expect(new URL(page.url()).hash).toBe('');
    expect(await message.status()).toMatchObject({ opensUsed: 0 });
    await page.unrouteAll({ behavior: 'ignoreErrors' });
  },
);

test('a pasted link reaches no browser store on disk, while a clicked one does (W2-01, W2-02)', async () => {
  const pasted = await createMessageThroughApi({ text: 'pasted, not clicked', opensMax: 2 });
  const clicked = await createMessageThroughApi({ text: 'clicked', opensMax: 2 });
  await withChromiumProfile(async (context, profile) => {
    const page = context.pages()[0] ?? (await context.newPage());
    const addresses: string[] = [];
    page.on('framenavigated', (frame) => {
      addresses.push(frame.url());
    });
    await openApp(page, `${BASE_URL}/`);
    await page.getByLabel('Link to open').fill(pasted.link);
    await page.getByRole('button', { name: 'Open', exact: true }).click();
    await page.getByRole('button', { name: 'Show message' }).click();
    await expectMessageShown(page);
    await expect(page.locator('pre')).toHaveText('pasted, not clicked');
    expect(addresses.filter((address) => address.includes('#'))).toEqual([]);
    const other = await context.newPage();
    await showMessage(other, clicked.link);
    await expectMessageShown(other);
    await context.close();
    expect(filesHolding(profile, pasted.secret)).toEqual([]);
    expect(filesHolding(profile, clicked.secret)).not.toEqual([]);
  });
});

test('Chromium offers to save neither passphrase (W2-03)', async () => {
  await withChromiumProfile(async (context) => {
    const internals = context.pages()[0] ?? (await context.newPage());
    await internals.goto('chrome://password-manager-internals');
    const sender = await context.newPage();
    await openApp(sender, `${BASE_URL}/`);
    await sender.getByLabel('Message text').fill('saved nowhere');
    await sender.getByRole('button', { name: 'Passphrase', exact: true }).click();
    await sender.getByRole('switch', { name: /^Protect with a passphrase/ }).click();
    const chosen = sender.getByLabel('Passphrase, optional');
    await chosen.clear();
    await chosen.pressSequentially(PASSPHRASE);
    await sender.getByRole('button', { name: 'Create link' }).click();
    const link = await sender.getByLabel('Link to the message').inputValue({ timeout: 45_000 });
    const recipient = await context.newPage();
    await recipient.goto(link);
    await recipient.getByLabel('Passphrase', { exact: true }).pressSequentially(PASSPHRASE);
    await recipient.getByRole('button', { name: 'Show message' }).click();
    await expectMessageShown(recipient);
    await recipient.waitForTimeout(2000);
    const log = await internals.evaluate(() => document.body.textContent);
    expect(log).toContain('PasswordManager');
    expect(log).not.toMatch(/Show password prompt|Decision: ASK/);
  });
});
