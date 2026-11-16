import type { Page } from '@playwright/test';

import { expect, test } from '../support/fixtures.ts';
import { createMessageThroughApi } from '../support/messages.ts';
import { openApp } from '../support/pages.ts';

const VISITS_PATH = '/api/v1/visits';

async function watchVisitSignals(page: Page): Promise<unknown[]> {
  // A person's browser reports no automation, which the page would otherwise report at once.
  await page.addInitScript(() => {
    Object.defineProperty(Navigator.prototype, 'webdriver', { get: () => false });
  });
  const signals: unknown[] = [];
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === VISITS_PATH) {
      signals.push(request.postDataJSON());
    }
  });
  return signals;
}

async function visitCodeLoaded(page: Page): Promise<boolean> {
  return page.evaluate(() =>
    performance.getEntriesByType('resource').some((entry) => /\/assets\/visits-[\w-]+\.js$/u.test(entry.name)),
  );
}

async function settle(page: Page): Promise<void> {
  await page.evaluate(
    async () =>
      new Promise<void>((resolve) => {
        setTimeout(resolve, 250);
      }),
  );
}

async function actLikeAPerson(page: Page): Promise<void> {
  await page.mouse.move(20, 20);
  await page.mouse.move(60, 80);
  await page.keyboard.press('Shift');
  await page.mouse.wheel(0, 120);
}

test('the first interaction of a person on the home page sends exactly one visit signal', async ({ page }) => {
  const signals = await watchVisitSignals(page);
  await openApp(page, '/');
  await expect.poll(async () => visitCodeLoaded(page)).toBe(true);
  expect(signals).toEqual([]);

  await actLikeAPerson(page);
  await expect.poll(() => signals).toEqual([{ signal: 'person', page: 'home' }]);
  await actLikeAPerson(page);
  await settle(page);
  expect(signals).toHaveLength(1);
});

test('a message page loads no visit code and sends no visit signal', async ({ page }) => {
  const signals = await watchVisitSignals(page);
  const message = await createMessageThroughApi();
  await page.goto(message.link);
  await page.locator('html[data-ready]').waitFor({ state: 'attached' });
  await actLikeAPerson(page);
  await settle(page);
  expect(signals).toEqual([]);
  expect(await visitCodeLoaded(page)).toBe(false);
});
