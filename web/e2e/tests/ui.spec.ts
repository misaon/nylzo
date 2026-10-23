import { readFileSync, readdirSync } from 'node:fs';
import { gzipSync } from 'node:zlib';

import AxeBuilder from '@axe-core/playwright';
import { KDF_PBKDF2 } from '@nylzo/core';
import type { Page } from '@playwright/test';
import { transform, type Declaration } from 'lightningcss';

import { BASE_URL, createViaApi, createViaUi, expect, expectRevealed, openViaUi, test } from './helpers.ts';

const ASSETS = new URL('../../../internal/server/web/dist/assets/', import.meta.url);
const MOBILE = { width: 430, height: 932 };
const DESKTOP = { width: 1440, height: 900 };
const MOTION = ['transform', 'opacity', 'translate', 'scale', 'rotate'];

declare global {
  interface Window {
    nylzoShifts: () => string[];
    nylzoResized: string[];
  }
}

async function watchLayout(page: Page): Promise<void> {
  await page.addInitScript(() => {
    type Shift = PerformanceEntry & { value: number; sources: { node?: Node | null; previousRect: DOMRectReadOnly; currentRect: DOMRectReadOnly }[] };
    const shifted: string[] = [];
    const describe = (rect: DOMRectReadOnly) => `${Math.round(rect.x)},${Math.round(rect.y)} ${Math.round(rect.width)}x${Math.round(rect.height)}`;
    const add = (entries: PerformanceEntry[]) => {
      for (const entry of entries as Shift[]) {
        const sources = entry.sources.map((source) => {
          const node = source.node instanceof Element ? `${source.node.tagName.toLowerCase()}#${source.node.id}` : String(source.node?.nodeName);
          return `${node} ${describe(source.previousRect)} -> ${describe(source.currentRect)}`;
        });
        shifted.push(`${entry.value.toFixed(4)}: ${sources.join('; ')}`);
      }
    };
    const shifts = new PerformanceObserver((list) => add(list.getEntries()));
    shifts.observe({ type: 'layout-shift', buffered: true });
    window.nylzoShifts = () => {
      add(shifts.takeRecords());
      return shifted.splice(0);
    };
    window.nylzoResized = [];
    const sizes = new WeakMap<Element, string>();
    const resizes = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const box = entry.borderBoxSize[0];
        if (!entry.target.isConnected || !box || box.inlineSize === 0) {
          continue;
        }
        const size = `${Math.round(box.inlineSize)}x${Math.round(box.blockSize)}`;
        const before = sizes.get(entry.target);
        if (before !== undefined && before !== size) {
          window.nylzoResized.push(`${entry.target.textContent} ${before} -> ${size}`);
        }
        sizes.set(entry.target, size);
      }
    });
    new MutationObserver(() => {
      for (const element of document.querySelectorAll('button, input, select, textarea')) {
        resizes.observe(element);
      }
    }).observe(document, { childList: true, subtree: true });
  });
}

async function settled(page: Page): Promise<void> {
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
}

async function expectStable(page: Page, screen: string): Promise<void> {
  await settled(page);
  expect(await page.evaluate(() => window.nylzoShifts()), screen).toEqual([]);
  expect(await page.evaluate(() => window.nylzoResized), screen).toEqual([]);
}

for (const viewport of [MOBILE, DESKTOP]) {
  test(`nothing shifts or resizes after loading or a click while creating and reading at ${viewport.width}x${viewport.height} (AK18)`, async ({ browser, browserName }) => {
    test.skip(browserName !== 'chromium', 'only Chromium implements the Layout Instability API');
    const passphrase = 'four random words here';
    const sender = await (await browser.newContext({ viewport })).newPage();
    await watchLayout(sender);
    await sender.goto('/');
    await expect(sender.getByLabel('Message text')).toBeVisible();
    await expectStable(sender, 'create loaded');
    await sender.getByLabel('Message text').fill('layout');
    await sender.getByLabel('Passphrase, optional').fill(passphrase);
    await sender.getByRole('radio', { name: /^PBKDF2/ }).check();
    await settled(sender);
    await sender.evaluate(() => window.nylzoShifts());
    await sender.getByRole('button', { name: 'Create link' }).click();
    await expect(sender.getByLabel('Link to the message')).toBeVisible({ timeout: 45_000 });
    await expectStable(sender, 'created');

    const recipient = await (await browser.newContext({ viewport })).newPage();
    await watchLayout(recipient);
    const message = await createViaApi({ text: 'layout', passphrase, kdfId: KDF_PBKDF2 });
    await recipient.goto(message.link);
    await expect(recipient.getByRole('button', { name: 'Show message' })).toBeVisible();
    await expectStable(recipient, 'read loaded');
    await recipient.getByLabel('Passphrase', { exact: true }).fill('definitely the wrong one');
    await recipient.getByRole('button', { name: 'Show message' }).click();
    await expect(recipient.getByText(/The passphrase does not match/)).toBeVisible({ timeout: 30_000 });
    await expectStable(recipient, 'rejected');
    await recipient.getByLabel('Passphrase', { exact: true }).fill(passphrase);
    await recipient.getByRole('button', { name: 'Show message' }).click();
    await expectRevealed(recipient);
    await expectStable(recipient, 'revealed');
  });
}

for (const viewport of [MOBILE, DESKTOP]) {
  test(`nothing shifts or resizes on the sent list, the report page and the read actions at ${viewport.width}x${viewport.height} (AK18)`, async ({ browser, browserName }) => {
    test.skip(browserName !== 'chromium', 'only Chromium implements the Layout Instability API');
    const sender = await (await browser.newContext({ viewport })).newPage();
    await createViaUi(sender, { text: 'first' });
    await createViaUi(sender, { text: 'second' });
    await watchLayout(sender);
    await sender.goto('/sent');
    await expect(sender.getByText('Waiting to be opened.')).toHaveCount(2);
    await expectStable(sender, 'sent loaded');
    await sender.getByRole('button', { name: 'Cancel message' }).first().click();
    await sender.getByRole('dialog').getByRole('button', { name: 'Yes, cancel' }).click();
    await expect(sender.getByText('Cancelled.', { exact: true })).toBeVisible();
    await expectStable(sender, 'cancelled');

    const reporter = await (await browser.newContext({ viewport })).newPage();
    await watchLayout(reporter);
    const message = await createViaApi({ text: 'layout', opensMax: 2 });
    await reporter.goto('/report');
    await expect(reporter.getByLabel('Link to the message')).toBeVisible();
    await expectStable(reporter, 'report loaded');
    await reporter.getByLabel('Link to the message').fill(message.link);
    await reporter.getByRole('button', { name: 'Send report' }).click();
    await expect(reporter.getByText('We received the report.')).toBeVisible();
    await expectStable(reporter, 'report accepted');

    const recipient = await (await browser.newContext({ viewport })).newPage();
    await watchLayout(recipient);
    await recipient.goto(message.link);
    await expect(recipient.getByRole('button', { name: 'Show message' })).toBeVisible();
    await expectStable(recipient, 'read loaded');
    await recipient.getByRole('button', { name: 'Report this message' }).click();
    await expect(recipient.getByLabel('Reason, optional')).toBeVisible();
    await expectStable(recipient, 'report form open');
    await recipient.getByRole('button', { name: 'Delete without showing' }).click();
    await recipient.getByRole('dialog').getByRole('button', { name: 'Do not delete' }).click();
    await expectStable(recipient, 'delete dismissed');
    await recipient.getByRole('button', { name: 'Show message' }).click();
    await expectRevealed(recipient);
    await expectStable(recipient, 'revealed with actions');
    await recipient.getByRole('button', { name: 'Delete the remaining opens' }).click();
    await recipient.getByRole('dialog').getByRole('button', { name: 'Yes, delete' }).click();
    await expect(recipient.getByRole('button', { name: 'Delete the remaining opens' })).toBeDisabled();
    await expectStable(recipient, 'rest deleted');
  });
}

async function reload(page: Page, url: string): Promise<void> {
  await page.goto('about:blank');
  await page.goto(url);
}

async function expectAccessible(page: Page, screen: string): Promise<void> {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
  expect(results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`), screen).toEqual([]);
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
  expect(small, screen).toEqual([]);
}

test('every screen passes axe and keeps touch targets at 44 px on a phone (AK20)', async ({ browser }) => {
  const context = await browser.newContext({ viewport: MOBILE, bypassCSP: true });
  const page = await context.newPage();
  await page.goto('/');
  await page.getByLabel('Passphrase, optional').fill('four random words here');
  await expectAccessible(page, 'create');
  await page.getByLabel('Message text').fill('axe');
  await page.getByLabel('Passphrase, optional').fill('short');
  await page.getByRole('button', { name: 'Create link' }).click();
  await expect(page.getByLabel('Passphrase, optional')).toBeFocused();
  await expectAccessible(page, 'create with problems');
  await createViaUi(page, { text: 'axe' });
  await expectAccessible(page, 'created');

  const message = await createViaApi({ text: 'axe' });
  await page.goto(message.link);
  await expect(page.getByRole('button', { name: 'Show message' })).toBeVisible();
  await expectAccessible(page, 'ready');
  await page.getByRole('button', { name: 'Show message' }).click();
  await expectRevealed(page);
  await expectAccessible(page, 'revealed');
  await reload(page, message.link);
  await expect(page.getByText('The message has already been opened.')).toBeVisible();
  await expectAccessible(page, 'opened');

  const guarded = await createViaApi({ passphrase: 'four random words here', kdfId: KDF_PBKDF2 });
  await openViaUi(page, guarded.link, 'definitely the wrong one');
  await expect(page.getByText(/The passphrase does not match/)).toBeVisible({ timeout: 30_000 });
  await expectAccessible(page, 'rejected');

  await page.goto(`${BASE_URL}/m/${message.idText}#2.0.${message.secret}`);
  await expect(page.getByText(/The link is incomplete or damaged/)).toBeVisible();
  await expectAccessible(page, 'invalid');
  await reload(page, `${BASE_URL}/m/${message.idText}#1.0.${'A'.repeat(43)}`);
  await expect(page.getByText(/does not exist or can no longer be opened/)).toBeVisible();
  await expectAccessible(page, 'unknown');
});

test('the confirmation before a risky download passes axe (AK20, D35)', async ({ browser }) => {
  const context = await browser.newContext({ viewport: MOBILE, bypassCSP: true });
  const page = await context.newPage();
  const link = await createViaUi(page, { file: { name: 'setup.exe', mimeType: 'application/octet-stream', buffer: Buffer.from('MZ') } });
  await openViaUi(page, link);
  await expectRevealed(page);
  await page.getByRole('button', { name: 'Download attachment' }).click();
  await expect(page.getByRole('button', { name: 'Download anyway' })).toBeVisible();
  await expectAccessible(page, 'confirm download');
});

test('the sent list and its dialog pass axe (AK20)', async ({ browser }) => {
  const context = await browser.newContext({ viewport: MOBILE, bypassCSP: true });
  const page = await context.newPage();
  await page.goto('/sent');
  await expect(page.getByText('This device remembers no messages.')).toBeVisible();
  await expectAccessible(page, 'sent empty');
  await createViaUi(page, { text: 'axe' });
  await expectAccessible(page, 'created and remembered');
  await page.goto('/sent');
  await expect(page.getByText('Waiting to be opened.')).toBeVisible();
  await expectAccessible(page, 'sent list');
  await page.getByRole('button', { name: 'Cancel message' }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await expectAccessible(page, 'cancel dialog');
  await page.getByRole('dialog').getByRole('button', { name: 'Keep it' }).click();
});

test('the report page passes axe with and without problems (AK20)', async ({ browser }) => {
  const context = await browser.newContext({ viewport: MOBILE, bypassCSP: true });
  const page = await context.newPage();
  await page.goto('/report');
  await expectAccessible(page, 'report');
  await page.getByLabel('Link to the message').fill('not a link');
  await page.getByLabel('Reason, optional').fill('x'.repeat(1001));
  await page.getByRole('button', { name: 'Send report' }).click();
  await expect(page.getByLabel('Link to the message')).toBeFocused();
  await expectAccessible(page, 'report with problems');
});

test('the read actions and the delete dialog pass axe (AK20)', async ({ browser }) => {
  const context = await browser.newContext({ viewport: MOBILE, bypassCSP: true });
  const page = await context.newPage();
  const message = await createViaApi({ text: 'axe', opensMax: 2 });
  await page.goto(message.link);
  await page.getByRole('button', { name: 'Report this message' }).click();
  await expectAccessible(page, 'read with report form');
  await page.getByRole('button', { name: 'Delete without showing' }).click();
  await expectAccessible(page, 'delete dialog');
  await page.getByRole('dialog').getByRole('button', { name: 'Do not delete' }).click();
  await page.getByRole('button', { name: 'Show message' }).click();
  await expectRevealed(page);
  await expectAccessible(page, 'revealed with actions');
  await page.getByRole('button', { name: 'Čeština' }).click();
  await expectAccessible(page, 'revealed in Czech');
});

function declarationName(declaration: Declaration): string {
  switch (declaration.property) {
    case 'unparsed':
      return declaration.value.propertyId.property;
    case 'custom':
      return declaration.value.name;
    default:
      return declaration.property;
  }
}

function motionProblems(css: string): { readonly problems: string[]; readonly animated: boolean } {
  const problems: string[] = [];
  let animated = false;
  const animates = (where: string, property: string) => {
    if (property === 'none') {
      return;
    }
    animated = true;
    if (!MOTION.includes(property)) {
      problems.push(`${where} animates ${property}`);
    }
  };
  transform({
    filename: 'bundle.css',
    code: Buffer.from(css),
    visitor: {
      Rule: {
        keyframes(rule) {
          for (const keyframe of rule.value.keyframes) {
            for (const declaration of [...(keyframe.declarations.declarations ?? []), ...(keyframe.declarations.importantDeclarations ?? [])]) {
              animates('@keyframes', declarationName(declaration));
            }
          }
        },
      },
      Declaration(declaration) {
        if (declaration.property === 'transition-property') {
          declaration.value.forEach((id) => animates('transition-property', id.property));
        } else if (declaration.property === 'transition') {
          declaration.value.forEach((transition) => animates('transition', transition.property.property));
        } else if (declaration.property === 'unparsed' && /^(?:transition|animation)/.test(declaration.value.propertyId.property)) {
          problems.push(`${declaration.value.propertyId.property} has a value the check cannot read`);
        }
      },
    },
  });
  return { problems, animated };
}

test('the motion check reads shorthands and keyframes (AK19)', ({ browserName }) => {
  test.skip(browserName !== 'chromium', 'runs in Node once');
  expect(motionProblems('a { transition: opacity .2s, transform .2s }').problems).toEqual([]);
  expect(motionProblems('a { transition: width 1s }').problems).toEqual(['transition animates width']);
  expect(motionProblems('a { transition: all 1s }').problems).toEqual(['transition animates all']);
  expect(motionProblems('a { transition-property: height }').problems).toEqual(['transition-property animates height']);
  expect(motionProblems('a { transition: var(--t) }').problems).toEqual(['transition has a value the check cannot read']);
  expect(motionProblems('@keyframes k { to { margin-left: 4px } }').problems).toEqual(['@keyframes animates margin-left']);
  expect(motionProblems('a { transition: none }')).toEqual({ problems: [], animated: false });
});

test('the bundle stays within budget and animates only transform and opacity (AK19, AK20)', async ({ browserName }) => {
  test.skip(browserName !== 'chromium', 'checks the build output once');
  const files = readdirSync(ASSETS).filter((name) => !name.endsWith('.br') && !name.endsWith('.gz'));
  const gzipped = (suffix: string) =>
    files.filter((name) => name.endsWith(suffix)).reduce((sum, name) => sum + gzipSync(readFileSync(new URL(name, ASSETS))).length, 0);
  expect(gzipped('.js')).toBeLessThanOrEqual(150 * 1024);
  expect(gzipped('.css')).toBeLessThanOrEqual(40 * 1024);
  const css = files.filter((name) => name.endsWith('.css')).map((name) => readFileSync(new URL(name, ASSETS), 'utf8')).join('\n');
  const { problems, animated } = motionProblems(css);
  expect(problems).toEqual([]);
  if (animated) {
    expect(css).toContain('prefers-reduced-motion');
  }
  const js = files.filter((name) => name.endsWith('.js')).map((name) => readFileSync(new URL(name, ASSETS), 'utf8')).join('\n');
  expect(js).not.toMatch(/\bnew Function\b|\beval\(|svelte\/compiler/);
});
