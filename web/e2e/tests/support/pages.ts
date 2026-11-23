import type { Locator, Page, Response } from '@playwright/test';

import { expect } from './fixtures.ts';
import type { MessageFile } from './messages.ts';
import { BASE_URL } from './servers.ts';

export interface ComposedMessage {
  readonly text?: string;
  readonly file?: MessageFile;
  readonly passphrase?: string;
  readonly remember?: 'browser' | 'tab';
}

export async function openApp(page: Page, url: string): Promise<Response | null> {
  const response = await page.goto(url);
  await page.locator('html[data-ready]').waitFor({ state: 'attached' });
  return response;
}

export async function createMessageInPage(page: Page, message: ComposedMessage, base = BASE_URL): Promise<string> {
  await openApp(page, `${base}/`);
  await page.getByLabel('Message text').fill(message.text ?? '');
  if (message.file) {
    await page.getByLabel(/File up to/u).setInputFiles(message.file);
  }
  if (message.passphrase) {
    await choosePassphrase(page, message.passphrase);
  }
  await page.getByRole('button', { name: 'Create link' }).click();
  const link = await createdLink(page);
  if (message.remember === 'browser') {
    await trackInSent(page);
  } else if (message.remember === 'tab') {
    await trackInTabAfterRefusedSave(page);
  }
  return link;
}

export async function createdLink(page: Page): Promise<string> {
  const link = page.getByLabel('Link to the message');
  await expect(link).toBeVisible({ timeout: 45_000 });
  return link.inputValue();
}

async function openSecurityMenu(page: Page): Promise<void> {
  const menu = page.getByRole('button', { name: /^(?:No|With) passphrase$/u });
  if ((await menu.getAttribute('aria-expanded')) !== 'true') {
    await menu.click();
  }
}

const TAB_REMEMBERS = 'This tab remembers the message, at most until you close or reload it.';

interface Tracking {
  readonly label?: string;
  readonly where?: 'browser' | 'tab';
}

export async function trackInSent(page: Page, { label = '', where = 'browser' }: Tracking = {}): Promise<void> {
  await page.getByRole('button', { name: 'Track in Sent' }).click();
  const labelField = page.getByLabel('Label, only you see it');
  await labelField.fill(label);
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(
    page.getByText(where === 'browser' ? /This device (?:remembered|could not remember) the message/u : TAB_REMEMBERS),
  ).toBeVisible();
  // The panel leaves 900 ms after a save and lifts the page under it; a press that straddles the lift clicks nothing.
  if (await page.getByText('This device could not remember the message').isHidden()) {
    await expect(labelField).toBeHidden();
  }
}

// The web app keeps a record in the tab only after the browser refused to store it.
async function trackInTabAfterRefusedSave(page: Page): Promise<void> {
  await page.evaluate(() => {
    Object.defineProperty(indexedDB, 'open', {
      configurable: true,
      value: () => {
        throw new DOMException('refused', 'SecurityError');
      },
    });
  });
  await page.getByRole('button', { name: 'Track in Sent' }).click();
  const save = page.getByRole('button', { name: 'Save', exact: true });
  await save.click();
  await expect(page.getByText('This device could not remember the message')).toBeVisible();
  await page.evaluate(() => Reflect.deleteProperty(indexedDB, 'open'));
  await save.click();
  await expect(page.getByText(TAB_REMEMBERS)).toBeVisible();
  await expect(page.getByLabel('Label, only you see it')).toBeHidden();
}

const LANGUAGE_CODES = { Čeština: 'cs', English: 'en' } as const;

export async function chooseLanguage(page: Page, name: keyof typeof LANGUAGE_CODES): Promise<void> {
  await page
    .getByRole('button', { name: /(?:Language|Jazyk): /u })
    .first()
    .click();
  const option = page.getByRole('option', { name });
  await expect(option).toHaveAttribute('lang', LANGUAGE_CODES[name]);
  await option.click();
}

export async function choosePassphrase(page: Page, passphrase: string): Promise<void> {
  await openSecurityMenu(page);
  const protect = page.getByRole('switch', { name: /^Protect with a passphrase/u });
  if ((await protect.getAttribute('aria-checked')) !== 'true') {
    await protect.click();
  }
  await page.getByLabel('Passphrase', { exact: true }).fill(passphrase);
}

interface Unlocking {
  readonly passphrase?: string;
}

export async function showMessage(page: Page, link: string, { passphrase }: Unlocking = {}): Promise<void> {
  await page.goto(link);
  if (passphrase !== undefined) {
    await page.getByLabel('Passphrase', { exact: true }).fill(passphrase);
  }
  await page.getByRole('button', { name: 'Show message' }).click();
}

export async function expectMessageShown(page: Page): Promise<void> {
  await expect(page.getByRole('heading', { name: 'Message content' })).toBeVisible({ timeout: 45_000 });
}

export async function leavePage(page: Page): Promise<void> {
  await page.evaluate(() => dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true })));
}

export async function returnFromBackForwardCache(page: Page): Promise<void> {
  const reloaded = page.waitForEvent('load');
  await page.evaluate(() => dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true })));
  await reloaded;
}

export async function nextFrames(page: Page): Promise<void> {
  await page.evaluate(async () => {
    await new Promise((resolve) => {
      requestAnimationFrame(resolve);
    });
    await new Promise((resolve) => {
      requestAnimationFrame(resolve);
    });
  });
}

export function notInert(page: Page, locator: Locator): Locator {
  return locator.and(page.locator(':not([inert]):not([inert] *)'));
}

export function deleteRestButton(page: Page): Locator {
  return page.getByRole('button', { name: 'Delete message' }).and(page.locator(':not([role="alertdialog"] *)'));
}
