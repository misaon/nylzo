import type { Page } from '@playwright/test';

import AxeBuilder from '@axe-core/playwright';

import { expect, test } from './support/fixtures.ts';
import { createMessageThroughApi } from './support/messages.ts';
import { choosePassphrase, createMessageInPage, expectMessageShown, openApp, showMessage } from './support/pages.ts';
import { BASE_URL } from './support/servers.ts';

const PHONE = { viewport: { width: 430, height: 932 }, bypassCSP: true };
const WCAG = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];
const STILL =
  '*, ::before, ::after { transition: none !important; animation: none !important; transform: none !important; translate: none !important; scale: none !important; rotate: none !important; }';

async function animationsFinished(page: Page): Promise<void> {
  await page.waitForFunction(() =>
    document
      .getAnimations()
      .every(
        (animation) =>
          !(animation.timeline instanceof DocumentTimeline) ||
          animation.effect?.getComputedTiming().iterations === Infinity ||
          animation.playState !== 'running',
      ),
  );
}

function smallTargets(page: Page, still: string): Promise<string[]> {
  return page.evaluate((style) => {
    const freeze = document.createElement('style');
    freeze.textContent = style;
    document.head.append(freeze);
    try {
      const targets = [...document.querySelectorAll<HTMLElement>('button, a[href], input, select, textarea')].map(
        (element) =>
          element instanceof HTMLInputElement && (element.type === 'radio' || element.type === 'checkbox')
            ? (element.closest('label') ?? element)
            : element,
      );
      return targets
        .filter((element) => element.getClientRects().length > 0)
        .map((element) => ({ element, box: element.getBoundingClientRect() }))
        .filter(({ box }) => box.width < 44 || box.height < 44)
        .map(({ element, box }) => {
          const name = element.getAttribute('aria-label') ?? element.textContent;
          return `${element.tagName.toLowerCase()} "${name.trim().slice(0, 60)}" ${box.width}x${box.height}`;
        });
    } finally {
      freeze.remove();
    }
  }, still);
}

async function expectAccessible(page: Page, screen: string, scope?: string): Promise<void> {
  await animationsFinished(page);
  const axe = new AxeBuilder({ page }).withTags(WCAG);
  const results = await (scope === undefined ? axe : axe.include(scope)).analyze();
  const violations = results.violations.map(
    (violation) => `${violation.id}: ${violation.nodes.map((node) => node.target.join(' ')).join(', ')}`,
  );
  expect(violations, screen).toEqual([]);
  expect(await smallTargets(page, STILL), screen).toEqual([]);
}

async function openFresh(page: Page, url: string): Promise<void> {
  await page.goto('about:blank');
  await page.goto(url);
}

test('the home page, its tabs, the composer popovers, an attachment and a refused passphrase pass axe with 44 px targets on a phone (AK20)', async ({
  newPage,
}) => {
  const page = await newPage(PHONE);
  await openApp(page, '/');
  await expectAccessible(page, 'home');

  const tabs = page.getByRole('tab');
  const tabCount = await tabs.count();
  expect(tabCount).toBeGreaterThan(0);
  for (let index = 0; index < tabCount; index++) {
    const tab = tabs.nth(index);
    await tab.click();
    await expect(tab).toHaveAttribute('aria-selected', 'true');
    const section = await tab.evaluate((element) => element.closest('section')?.id ?? '');
    expect(section).not.toBe('');
    await expectAccessible(page, `tab ${index}`, `#${section}`);
  }

  const popoverTriggers = page.locator('#create button[aria-haspopup="dialog"]');
  await expect(popoverTriggers).toHaveCount(3);
  for (let index = 0; index < 3; index++) {
    const trigger = popoverTriggers.nth(index);
    await trigger.click();
    await expect(trigger).toHaveAttribute('aria-expanded', 'true');
    await expectAccessible(page, `popover ${index}`, '#create');
    await page.keyboard.press('Escape');
    await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  }

  await page
    .getByLabel(/Attachment, up to/)
    .setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('notes') });
  await expect(page.getByText('notes.txt')).toBeVisible();
  await expectAccessible(page, 'attachment', '#create');

  await page.getByLabel('Message text').fill('axe');
  await choosePassphrase(page, 'short');
  await page.getByRole('button', { name: 'Create link' }).click();
  await expect(page.getByLabel('Passphrase, optional')).toBeFocused();
  await expectAccessible(page, 'refused passphrase', '#create');
});

test('the read page in every state, with its report form and its delete and download confirmations, passes axe with 44 px targets on a phone (AK20)', async ({
  newPage,
}) => {
  const page = await newPage(PHONE);
  const message = await createMessageThroughApi({ text: 'axe', opensMax: 2 });
  const show = page.getByRole('button', { name: 'Show message' });
  await page.goto(message.link);
  await expect(show).toBeVisible();
  await expectAccessible(page, 'ready');
  await page.getByRole('button', { name: 'Report', exact: true }).click();
  await expectAccessible(page, 'report form');
  await page.getByRole('button', { name: 'Delete without showing' }).click();
  await expectAccessible(page, 'delete confirmation');
  await page.getByRole('alertdialog').getByRole('button', { name: 'Do not delete' }).click();
  await show.click();
  await expectMessageShown(page);
  await expectAccessible(page, 'revealed with actions');
  await page.getByRole('button', { name: 'Čeština' }).click();
  await expect(page.getByRole('heading', { name: 'Obsah zprávy' })).toBeVisible();
  await expectAccessible(page, 'revealed in Czech');
  await page.getByRole('button', { name: 'English' }).click();

  const risky = await createMessageThroughApi({
    file: { name: 'setup.exe', mimeType: 'application/octet-stream', buffer: Buffer.from('MZ') },
  });
  await openFresh(page, risky.link);
  await show.click();
  await expectMessageShown(page);
  await page.getByRole('button', { name: 'Download attachment' }).click();
  await expect(page.getByRole('button', { name: 'Download anyway' })).toBeVisible();
  await expectAccessible(page, 'download confirmation');
  await openFresh(page, risky.link);
  await expect(page.getByText('The message has already been opened')).toBeVisible();
  await expectAccessible(page, 'opened');

  const guarded = await createMessageThroughApi({ passphrase: 'four random words here' });
  await showMessage(page, guarded.link, 'definitely the wrong one');
  await expect(page.getByText(/The passphrase does not match/)).toBeVisible({ timeout: 30_000 });
  await expectAccessible(page, 'rejected');

  await openFresh(page, `${BASE_URL}/r#0.0.${message.id}.${message.secret}`);
  await expect(page.getByText(/The link is incomplete or damaged/)).toBeVisible();
  await expectAccessible(page, 'damaged link');
});

test('the sent list, empty and with a message, and its cancel dialog pass axe with 44 px targets on a phone (AK20)', async ({
  newPage,
}) => {
  const page = await newPage(PHONE);
  await page.goto('/sent');
  await expect(page.getByText('This device remembers no messages.')).toBeVisible();
  await expectAccessible(page, 'sent empty');
  await createMessageInPage(page, { text: 'axe', remember: true });
  await expectAccessible(page, 'created and remembered');
  await page.goto('/sent');
  await expect(page.getByText('Waiting to be opened.')).toBeVisible();
  await expectAccessible(page, 'sent list');
  await page.getByRole('button', { name: 'Cancel message' }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await expectAccessible(page, 'cancel dialog');
});

test('the report page with and without problems and the transparency page pass axe with 44 px targets on a phone (AK20)', async ({
  newPage,
}) => {
  const page = await newPage(PHONE);
  await page.goto('/report');
  await expectAccessible(page, 'report');
  await page.getByLabel('Link to the message').fill('not a link');
  await page.getByLabel('Reason, optional').fill('x'.repeat(1001));
  await page.getByRole('button', { name: 'Send report' }).click();
  await expect(page.getByLabel('Link to the message')).toBeFocused();
  await expectAccessible(page, 'report with problems');
  await page.goto('/transparency');
  await expect(page.getByText('Nylzo e2e')).toBeVisible();
  await expectAccessible(page, 'transparency');
});
