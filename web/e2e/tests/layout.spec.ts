import type { Locator, Page } from '@playwright/test';

import { expect, test } from './support/fixtures.ts';
import { createMessageThroughApi } from './support/messages.ts';
import {
  choosePassphrase,
  createdLink,
  createMessageInPage,
  expectMessageShown,
  nextFrames,
  openApp,
} from './support/pages.ts';

interface LayoutChanges {
  readonly shifts: string[];
  readonly resizes: string[];
}

declare global {
  var nylzoLayoutChanges: (() => LayoutChanges) | undefined;
}

const VIEWPORTS = [
  { width: 430, height: 932 },
  { width: 1440, height: 900 },
];
const FULL_MOTION = { reducedMotion: 'no-preference' } as const;
const PASSPHRASE = 'four random words here';
const LONG_TEXT = [
  'Ahoj, posílám přístup k serveru, jak jsme se domluvili včera večer.',
  'Heslo platí jen do pátku, pak ho prosím změň a starý záznam smaž.',
  'Díky!',
].join('\n');

async function watchLayout(page: Page): Promise<void> {
  await page.addInitScript(() => {
    interface LayoutShift extends PerformanceEntry {
      readonly value: number;
      readonly sources: { node?: Node | null; previousRect: DOMRectReadOnly; currentRect: DOMRectReadOnly }[];
    }
    const shifts: string[] = [];
    const resizes: string[] = [];
    // oxlint-disable-next-line unicorn/consistent-function-scoping -- Playwright serializes this closure into the browser, where module helpers are unavailable.
    const rectangle = (box: DOMRectReadOnly) =>
      `${Math.round(box.x)},${Math.round(box.y)} ${Math.round(box.width)}x${Math.round(box.height)}`;
    const recordShifts = (entries: PerformanceEntry[]) => {
      for (const entry of entries as LayoutShift[]) {
        const sources = entry.sources.map((source) => {
          const node =
            source.node instanceof Element
              ? `${source.node.tagName.toLowerCase()}#${source.node.id}`
              : String(source.node?.nodeName);
          return `${node} ${rectangle(source.previousRect)} -> ${rectangle(source.currentRect)}`;
        });
        shifts.push(`${entry.value.toFixed(4)}: ${sources.join('; ')}`);
      }
    };
    const shiftObserver = new PerformanceObserver((list) => {
      recordShifts(list.getEntries());
    });
    shiftObserver.observe({ type: 'layout-shift', buffered: true });
    const sizes = new WeakMap<Element, string>();
    const resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const box = entry.borderBoxSize[0];
        if (!box || box.inlineSize === 0 || !entry.target.isConnected) {
          continue;
        }
        const size = `${Math.round(box.inlineSize)}x${Math.round(box.blockSize)}`;
        const before = sizes.get(entry.target);
        if (before !== undefined && before !== size) {
          resizes.push(`${entry.target.textContent} ${before} -> ${size}`);
        }
        sizes.set(entry.target, size);
      }
    });
    new MutationObserver(() => {
      for (const element of document.querySelectorAll('button, input, select, textarea')) {
        resizeObserver.observe(element);
      }
    }).observe(document, { childList: true, subtree: true });
    Object.defineProperty(globalThis, 'nylzoLayoutChanges', {
      value: () => {
        recordShifts(shiftObserver.takeRecords());
        return { shifts: shifts.splice(0), resizes: [...resizes] };
      },
    });
  });
}

async function layoutChanges(page: Page): Promise<LayoutChanges | undefined> {
  await nextFrames(page);
  return page.evaluate(() => globalThis.nylzoLayoutChanges?.());
}

// Bounding boxes include transforms, so a position read during the entrance animation is offset.
async function motionFinished(page: Page): Promise<void> {
  await page.waitForFunction(() =>
    document
      .getAnimations()
      .every(
        (animation) =>
          animation.playState !== 'running' ||
          animation.timeline !== document.timeline ||
          animation.effect?.getComputedTiming().endTime === Infinity,
      ),
  );
}

async function pagePosition(locator: Locator): Promise<string> {
  await motionFinished(locator.page());
  return locator.evaluate((element) => {
    const box = element.getBoundingClientRect();
    return `${Math.round(box.x + scrollX)},${Math.round(box.y + scrollY)} ${Math.round(box.width)}x${Math.round(box.height)}`;
  });
}

// The card grows into the created panel and shrinks to the revealed message by design (owner decision R11).
async function settle(page: Page): Promise<void> {
  await expect
    .poll(
      async () => {
        const changes = await layoutChanges(page);
        return changes?.shifts;
      },
      { intervals: [1000] },
    )
    .toEqual([]);
}

async function expectNothingMoved(page: Page, screen: string): Promise<void> {
  expect(await layoutChanges(page), screen).toEqual({ shifts: [], resizes: [] });
}

for (const viewport of VIEWPORTS) {
  test(`nothing shifts or resizes after loading or a click while creating and reading at ${viewport.width}x${viewport.height} (AK18)`, async ({
    newPage,
  }) => {
    const sender = await newPage({ viewport, ...FULL_MOTION });
    await watchLayout(sender);
    await openApp(sender, '/');
    await expect(sender.getByLabel('Message text')).toBeVisible();
    await expectNothingMoved(sender, 'create loaded');
    await sender.getByLabel('Message text').fill('layout');
    await choosePassphrase(sender, PASSPHRASE);
    await layoutChanges(sender);
    await sender.getByRole('button', { name: 'Create link' }).click();
    await createdLink(sender);
    await settle(sender);
    await expectNothingMoved(sender, 'created');

    const recipient = await newPage({ viewport, ...FULL_MOTION });
    await watchLayout(recipient);
    const message = await createMessageThroughApi({ text: LONG_TEXT, passphrase: PASSPHRASE });
    const passphrase = recipient.getByLabel('Passphrase', { exact: true });
    const show = recipient.getByRole('button', { name: 'Show message' });
    await recipient.goto(message.link);
    await expect(show).toBeVisible();
    await expectNothingMoved(recipient, 'read loaded');
    await passphrase.fill('definitely the wrong one');
    await show.click();
    await expect(recipient.getByText(/Wrong passphrase/u)).toBeVisible({ timeout: 30_000 });
    await expectNothingMoved(recipient, 'rejected');
    await passphrase.fill(PASSPHRASE);
    await show.click();
    await expectMessageShown(recipient);
    await expect(recipient.locator('pre')).toHaveText(LONG_TEXT);
    await settle(recipient);
    await expectNothingMoved(recipient, 'revealed and unscrambled');
  });

  test(`nothing shifts or resizes on the sent list and the read actions at ${viewport.width}x${viewport.height} (AK18)`, async ({
    newPage,
  }) => {
    const sender = await newPage({ viewport });
    await createMessageInPage(sender, { text: 'first', remember: 'browser' });
    await createMessageInPage(sender, { text: 'second', remember: 'browser' });
    await sender.emulateMedia(FULL_MOTION);
    await watchLayout(sender);
    await sender.goto('/sent');
    await expect(sender.getByText('Waiting to be opened').filter({ visible: true })).toHaveCount(2);
    await expectNothingMoved(sender, 'sent loaded');
    // A cancelled item takes its ended form by design (R26), so only the items above it must stay in place.
    const cancel = sender.getByRole('button', { name: 'Cancel message' }).last();
    await cancel.scrollIntoViewIfNeeded();
    const firstItem = sender.getByRole('listitem').first();
    const before = await pagePosition(firstItem);
    await cancel.click();
    await sender.getByRole('dialog').getByRole('button', { name: 'Yes, cancel' }).click();
    await expect(sender.getByText('Cancelled', { exact: true })).toBeVisible();
    await settle(sender);
    expect(await pagePosition(firstItem), 'cancelled').toEqual(before);

    const message = await createMessageThroughApi({ text: 'layout', opensMax: 2 });
    const recipient = await newPage({ viewport, ...FULL_MOTION });
    await watchLayout(recipient);
    await recipient.goto(message.link);
    await expect(recipient.getByRole('button', { name: 'Show message' })).toBeVisible();
    await expectNothingMoved(recipient, 'read loaded');
    await recipient.getByRole('button', { name: 'Delete without opening' }).click();
    await recipient.getByRole('alertdialog').getByRole('button', { name: 'Do not delete' }).click();
    await expectNothingMoved(recipient, 'delete dismissed');
    await recipient.getByRole('button', { name: 'Show message' }).click();
    await expectMessageShown(recipient);
    await settle(recipient);
    await expectNothingMoved(recipient, 'revealed with actions');
    await recipient.getByRole('button', { name: 'Delete message' }).click();
    await recipient.getByRole('alertdialog').getByRole('button', { name: 'Yes, delete' }).click();
    await expect(recipient.getByRole('button', { name: 'Delete message' })).toBeDisabled();
    await expectNothingMoved(recipient, 'rest deleted');
  });
}
