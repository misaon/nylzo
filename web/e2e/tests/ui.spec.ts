import { readFileSync, readdirSync } from 'node:fs';
import { gzipSync } from 'node:zlib';

import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

import { createViaApi, createViaUi, expectRevealed, openViaUi } from './helpers.ts';

const ASSETS = new URL('../../../internal/server/web/dist/assets/', import.meta.url);
const MOBILE = { width: 430, height: 932 };
const DESKTOP = { width: 1440, height: 900 };

declare global {
  interface Window {
    nylzoShift: number;
  }
}

async function watchLayoutShifts(page: Page): Promise<void> {
  await page.addInitScript(() => {
    window.nylzoShift = 0;
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries() as (PerformanceEntry & { value: number; hadRecentInput: boolean })[]) {
        if (!entry.hadRecentInput) {
          window.nylzoShift += entry.value;
        }
      }
    }).observe({ type: 'layout-shift', buffered: true });
  });
}

for (const viewport of [MOBILE, DESKTOP]) {
  test(`no layout shift while creating and reading at ${viewport.width}x${viewport.height} (AK18)`, async ({ browser, browserName }) => {
    test.skip(browserName !== 'chromium', 'only Chromium implements the Layout Instability API');
    const sender = await (await browser.newContext({ viewport })).newPage();
    const recipient = await (await browser.newContext({ viewport })).newPage();
    await watchLayoutShifts(sender);
    await watchLayoutShifts(recipient);
    const link = await createViaUi(sender, { text: 'layout' });
    await openViaUi(recipient, link);
    await expectRevealed(recipient);
    await recipient.waitForTimeout(600);
    expect(await sender.evaluate(() => window.nylzoShift)).toBe(0);
    expect(await recipient.evaluate(() => window.nylzoShift)).toBe(0);
  });
}

async function expectAccessible(page: Page): Promise<void> {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
  expect(results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
}

async function expectTouchTargets(page: Page): Promise<void> {
  const small = await page.evaluate(() => {
    const targets = [...document.querySelectorAll<HTMLElement>('button, a[href], input, select, textarea')].map((element) =>
      element instanceof HTMLInputElement && (element.type === 'radio' || element.type === 'checkbox') ? (element.closest('label') ?? element) : element,
    );
    return targets
      .filter((element) => element.getClientRects().length > 0)
      .map((element) => ({ element: element.outerHTML.slice(0, 80), box: element.getBoundingClientRect() }))
      .filter(({ box }) => box.width < 44 || box.height < 44)
      .map(({ element, box }) => `${element} ${Math.round(box.width)}x${Math.round(box.height)}`);
  });
  expect(small).toEqual([]);
}

test('screens pass axe and keep touch targets at 44 px on a phone (AK20)', async ({ browser }) => {
  const context = await browser.newContext({ viewport: MOBILE, bypassCSP: true });
  const page = await context.newPage();
  await page.goto('/');
  await page.getByLabel('Passphrase, optional').fill('four random words here');
  await expectAccessible(page);
  await expectTouchTargets(page);
  const message = await createViaApi({ text: 'axe' });
  await page.goto(message.link);
  await expect(page.getByRole('button', { name: 'Show message' })).toBeVisible();
  await expectAccessible(page);
  await expectTouchTargets(page);
  await page.getByRole('button', { name: 'Show message' }).click();
  await expectRevealed(page);
  await expectAccessible(page);
});

test('the bundle stays within budget and animates only transform and opacity (AK19, AK20)', async ({ browserName }) => {
  test.skip(browserName !== 'chromium', 'checks the build output once');
  const files = readdirSync(ASSETS);
  const gzipped = (suffix: string) =>
    files.filter((name) => name.endsWith(suffix)).reduce((sum, name) => sum + gzipSync(readFileSync(new URL(name, ASSETS))).length, 0);
  expect(gzipped('.js')).toBeLessThanOrEqual(150 * 1024);
  expect(gzipped('.css')).toBeLessThanOrEqual(40 * 1024);
  const css = files.filter((name) => name.endsWith('.css')).map((name) => readFileSync(new URL(name, ASSETS), 'utf8')).join('\n');
  const keyframes = [...css.matchAll(/@keyframes[^{]+\{((?:[^{}]*\{[^{}]*\})*)\s*\}/g)].map((match) => match[1] ?? '');
  for (const body of keyframes) {
    for (const [, property] of body.matchAll(/([a-z-]+)\s*:/g)) {
      expect(['transform', 'opacity', 'translate', 'scale', 'rotate']).toContain(property);
    }
  }
  for (const [, value] of css.matchAll(/transition-property\s*:\s*([^;}]+)/g)) {
    for (const property of (value ?? '').split(',').map((p) => p.trim())) {
      expect(['transform', 'opacity', 'translate', 'scale', 'rotate', 'none', 'var(--tw-transition-property)']).toContain(property);
    }
  }
  if (keyframes.length > 0 || /transition(-property)?\s*:/.test(css)) {
    expect(css).toContain('prefers-reduced-motion');
  }
  const js = files.filter((name) => name.endsWith('.js')).map((name) => readFileSync(new URL(name, ASSETS), 'utf8')).join('\n');
  expect(js).not.toMatch(/\bnew Function\b|\beval\(|svelte\/compiler/);
});
