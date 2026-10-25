import { createViaUi, expect, openApp, setPassphrase, test } from './helpers.ts';

const PHONE = { width: 430, height: 932 };

test('the composer forgets the text, the passphrase and the file once the link exists (SEC-8)', async ({ page }) => {
  const link = await createViaUi(page, {
    text: 'forget me',
    passphrase: 'four random words here',
    file: { name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('notes') },
  });
  expect(link).toContain('#1.1.');
  await expect(page.getByLabel('Message text')).toHaveValue('');
  await expect(page.getByLabel('Passphrase, optional')).toHaveValue('');
  expect(
    await page.getByLabel(/Attachment, up to/).evaluate((input: HTMLInputElement) => input.files?.length ?? 0),
  ).toBe(0);

  await page.getByRole('button', { name: 'New message' }).click();
  await expect(page.getByLabel('Message text')).toBeFocused();
  await expect(page.getByLabel('Message text')).toHaveValue('');
  await expect(page.getByText('notes.txt')).toHaveCount(0);
  await expect(page.getByRole('switch', { name: /^Protect with a passphrase/ })).toHaveAttribute(
    'aria-checked',
    'false',
  );
});

test('a refused create keeps the message and hands focus back to Create link', async ({ page }) => {
  await page.route('**/api/v1/messages', (route) =>
    route.request().method() === 'POST'
      ? route.fulfill({
          status: 429,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'rate_limited', retry_after: 30 }),
        })
      : route.continue(),
  );
  await openApp(page, '/');
  await page.getByLabel('Message text').fill('try again later');
  const submit = page.getByRole('button', { name: 'Create link' });
  await submit.click();
  await expect(
    page.getByRole('alert').filter({ hasText: /^Too many messages in a short time\. Try again in / }),
  ).toBeVisible({ timeout: 45_000 });
  await expect(submit).toBeEnabled();
  await expect(submit).toBeFocused();
  await expect(page.getByLabel('Message text')).toHaveValue('try again later');

  await page.unroute('**/api/v1/messages');
  await submit.click();
  await expect(page.getByLabel('Link to the message')).toBeVisible({ timeout: 45_000 });
});

test('on a phone the created panel copies the link and starts a new message', async ({ browser }) => {
  const page = await (await browser.newContext({ viewport: PHONE })).newPage();
  await page.addInitScript(() => {
    const copied: string[] = [];
    Object.defineProperty(window, 'nylzoCopied', { value: copied });
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: (text: string) => {
          copied.push(text);
          return Promise.resolve();
        },
      },
    });
  });
  await openApp(page, '/');
  await page.getByLabel('Message text').fill('phone');
  await setPassphrase(page, 'four random words here');
  await page.getByRole('button', { name: 'Create link' }).click();
  const link = page.getByLabel('Link to the message');
  await expect(link).toBeVisible({ timeout: 45_000 });
  await page.getByRole('button', { name: 'Copy link' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'The link is on the clipboard' })).toHaveCount(1);
  expect(await page.evaluate(() => (window as unknown as { nylzoCopied: string[] }).nylzoCopied)).toEqual([
    await link.inputValue(),
  ]);

  const another = page.getByRole('button', { name: 'New message' });
  const box = await another.boundingBox();
  expect(box?.height).toBeGreaterThanOrEqual(44);
  await another.click();
  await expect(page.getByLabel('Message text')).toBeFocused();
  await expect(page.getByLabel('Message text')).toHaveValue('');
});

test.describe('with full motion', () => {
  test.use({ reducedMotion: 'no-preference' });

  test('the derivation step keeps its passphrase wording until the link is ready', async ({ page }) => {
    await openApp(page, '/');
    await page.getByLabel('Message text').fill('scrypt wording');
    await setPassphrase(page, 'four random words here');
    await page.evaluate(() => {
      let plain = false;
      Object.defineProperty(window, 'nylzoPlainDerive', { get: () => plain });
      new MutationObserver(() => {
        plain ||= document.body.textContent?.includes('HKDF-SHA-256 creates the content key') ?? false;
      }).observe(document.body, { childList: true, subtree: true, characterData: true });
    });
    await page.getByRole('button', { name: 'Create link' }).click();
    await expect(page.getByLabel('Link to the message')).toBeVisible({ timeout: 45_000 });
    expect(await page.evaluate(() => (window as unknown as { nylzoPlainDerive: boolean }).nylzoPlainDerive)).toBe(
      false,
    );
  });

  test('the points of view cycle on their own, pause for the keyboard and stop for good after a pick (WCAG 2.2.2)', async ({
    page,
  }) => {
    await page.clock.install();
    await openApp(page, '/');
    const tabs = page.getByRole('tablist', { name: 'Point of view' });
    await tabs.evaluate((list) => list.closest('[role="group"]')?.scrollIntoView({ block: 'center' }));
    await expect(tabs.locator('progress')).toHaveClass(/opacity-100/);
    const selected = () => tabs.getByRole('tab', { selected: true }).textContent();
    const after = async (ms: number) => {
      for (let passed = 0; passed < ms; passed += 1000) {
        await page.clock.runFor(1000);
      }
      return selected();
    };
    const start = await selected();
    await expect.poll(() => after(1000), { timeout: 30_000 }).not.toBe(start);

    const current = tabs.getByRole('tab', { selected: true });
    await current.focus();
    expect(await current.evaluate((tab) => tab.matches(':focus-visible'))).toBe(true);
    const focused = await selected();
    expect(await after(20_000)).toBe(focused);

    const picked = tabs.getByRole('tab').first();
    await picked.click();
    await page.mouse.move(0, 0);
    await page.locator('h1').click();
    await after(30_000);
    await expect(picked).toHaveAttribute('aria-selected', 'true');
  });
});
