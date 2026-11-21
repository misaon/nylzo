import { setTimeout as delay } from 'node:timers/promises';

import type { Page } from '@playwright/test';

import { expect, test } from '../support/fixtures.ts';
import { openApp } from '../support/pages.ts';

const PHONE = { viewport: { width: 430, height: 932 }, hasTouch: true, reducedMotion: 'no-preference' } as const;
const DRAG_STEPS = 6;
const DRAG_STEP_MS = 12;
// Past half its height the sheet leaves at any speed, so the runner's pace decides nothing.
const DRAG_FRACTION = 0.75;

interface Drag {
  readonly fromY: number;
  readonly distance: number;
}

async function dragDown(page: Page, { fromY, distance }: Drag): Promise<void> {
  const session = await page.context().newCDPSession(page);
  const x = PHONE.viewport.width / 2;
  await session.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y: fromY }] });
  for (let step = 1; step <= DRAG_STEPS; step++) {
    await session.send('Input.dispatchTouchEvent', {
      type: 'touchMove',
      touchPoints: [{ x, y: fromY + (distance * step) / DRAG_STEPS }],
    });
    await delay(DRAG_STEP_MS);
  }
  await session.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
}

test('on a phone a composer menu opens as a sheet inside the viewport, and a drag down or a tap outside closes it', async ({
  newPage,
}) => {
  const page = await newPage(PHONE);
  await openApp(page, '/');
  const trigger = page.getByRole('button', { name: /Message expiry/u });
  const sheet = page.locator('.bottom-sheet:popover-open');

  await trigger.tap();
  await expect(sheet).toBeInViewport({ ratio: 1 });
  await expect(sheet.getByRole('button', { name: '7 days' })).toBeVisible();

  const box = await sheet.boundingBox();
  expect(box).not.toBeNull();
  await dragDown(page, { fromY: (box?.y ?? 0) + 20, distance: (box?.height ?? 0) * DRAG_FRACTION });
  await expect(sheet).toHaveCount(0);
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');

  await trigger.tap();
  await expect(sheet).toBeInViewport({ ratio: 1 });
  await page.touchscreen.tap(PHONE.viewport.width / 2, 120);
  await expect(sheet).toHaveCount(0);

  await trigger.tap();
  await sheet.getByRole('button', { name: '7 days' }).tap();
  await expect(sheet).toHaveCount(0);
  await expect(trigger).toContainText('7 days');
});
