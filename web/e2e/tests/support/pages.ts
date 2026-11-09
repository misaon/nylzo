import type { Locator, Page, Response } from '@playwright/test';

import { expect } from './fixtures.ts';
import type { MessageFile } from './messages.ts';
import { BASE_URL } from './servers.ts';

export interface ComposedMessage {
  readonly text?: string;
  readonly file?: MessageFile;
  readonly passphrase?: string;
  readonly remember?: boolean;
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
  if (message.remember) {
    await trackInSent(page);
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

export async function trackInSent(page: Page, label = ''): Promise<void> {
  await page.getByLabel('Label, only you see it').fill(label);
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByText(/This device (?:remembered|could not remember) the message/u)).toBeVisible();
}

export async function chooseLanguage(page: Page, name: 'Čeština' | 'English'): Promise<void> {
  await page
    .getByRole('button', { name: /(?:Language|Jazyk): /u })
    .first()
    .click();
  const option = page.getByRole('option', { name });
  await expect(option).toHaveAttribute('lang', name === 'English' ? 'en' : 'cs');
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

export async function showMessage(page: Page, link: string, passphrase?: string): Promise<void> {
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
