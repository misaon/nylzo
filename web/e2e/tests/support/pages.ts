import type { Locator, Page, Response } from '@playwright/test';

import type { MessageFile } from './messages.ts';

import { expect } from './fixtures.ts';
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
    await page.getByLabel(/Attachment, up to/).setInputFiles(message.file);
  }
  if (message.passphrase) {
    await choosePassphrase(page, message.passphrase);
  }
  if (message.remember) {
    await chooseRemember(page);
  }
  await page.getByRole('button', { name: 'Create link' }).click();
  return createdLink(page);
}

export async function createdLink(page: Page): Promise<string> {
  const link = page.getByLabel('Link to the message');
  await expect(link).toBeVisible({ timeout: 45_000 });
  return link.inputValue();
}

export async function chooseRemember(page: Page): Promise<void> {
  const remember = page.getByRole('switch', { name: /^Cancel from this device/ });
  const chip = page.getByRole('button', { name: 'Passphrase', exact: true });
  if ((await chip.getAttribute('aria-expanded')) !== 'true') {
    await chip.click();
  }
  if ((await remember.getAttribute('aria-checked')) !== 'true') {
    await remember.click();
  }
  await expect(remember).toHaveAttribute('aria-checked', 'true');
}

export async function choosePassphrase(page: Page, passphrase: string): Promise<void> {
  await page.getByRole('button', { name: 'Passphrase', exact: true }).click();
  const protect = page.getByRole('switch', { name: /^Protect with a passphrase/ });
  if ((await protect.getAttribute('aria-checked')) !== 'true') {
    await protect.click();
  }
  await page.getByLabel('Passphrase, optional').fill(passphrase);
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
    await new Promise((resolve) => requestAnimationFrame(resolve));
    await new Promise((resolve) => requestAnimationFrame(resolve));
  });
}

export function notInert(page: Page, locator: Locator): Locator {
  return locator.and(page.locator(':not([inert]):not([inert] *)'));
}
