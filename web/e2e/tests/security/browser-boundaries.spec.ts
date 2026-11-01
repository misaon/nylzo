import { expect, test } from '../support/fixtures.ts';
import { createMessageThroughApi } from '../support/messages.ts';
import { expectMessageShown, openApp, showMessage } from '../support/pages.ts';
import { BASE_URL, POW_BASE_URL } from '../support/servers.ts';

test(
  'the browser is cross-origin isolated and receives the required security policies',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const response = await openApp(page, '/');
    const headers = response?.headers() ?? {};
    expect(headers['cross-origin-opener-policy']).toBe('same-origin');
    expect(headers['cross-origin-embedder-policy']).toBe('require-corp');
    expect(headers['cross-origin-resource-policy']).toBe('same-origin');
    expect(headers['strict-transport-security']).toBe('max-age=63072000; includeSubDomains; preload');
    const features = headers['permissions-policy']?.split(', ') ?? [];
    for (const feature of [
      'camera',
      'microphone',
      'geolocation',
      'display-capture',
      'payment',
      'clipboard-read',
      'local-network',
      'loopback-network',
      'language-model',
      'publickey-credentials-get',
      'usb',
      'unload',
    ]) {
      expect(features).toContain(`${feature}=()`);
    }
    expect(features).toContain('clipboard-write=(self)');
    expect(features).toContain('web-share=(self)');
    expect(await page.evaluate(() => crossOriginIsolated)).toBe(true);
  },
);

test('same-origin browser mutations require the custom request header', { tag: '@cross-browser' }, async ({ page }) => {
  await openApp(page, '/');
  const statuses = await page.evaluate(async () => {
    const missing = await fetch('/api/v1/messages', { method: 'POST' });
    const marked = await fetch('/api/v1/messages', {
      method: 'POST',
      headers: { 'X-Requested-With': 'XMLHttpRequest' },
    });
    return { missing: missing.status, marked: marked.status };
  });
  expect(statuses).toEqual({ missing: 403, marked: 400 });
});

test(
  'a foreign form cannot submit a mutation or expose the application in a frame',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const instancePaths: string[] = [];
    page.on('response', (response) => {
      const url = new URL(response.url());
      if (url.origin === BASE_URL) {
        instancePaths.push(url.pathname);
      }
    });
    await page.route(`${POW_BASE_URL}/browser-attack`, (route) =>
      route.fulfill({
        contentType: 'text/html',
        body: `<!doctype html><meta charset="utf-8"><form method="post" action="${BASE_URL}/api/v1/messages" target="result"><input name="X-Requested-With" value="XMLHttpRequest"><button>Submit form</button></form><iframe name="result"></iframe><iframe title="embedded app" src="${BASE_URL}/"></iframe>`,
      }),
    );
    await page.goto(`${POW_BASE_URL}/browser-attack`);
    // The framed page arrives, and a rendered one would load its module scripts before the outer load event.
    expect(instancePaths).toEqual(['/']);
    const statuses: number[] = [];
    await page.route(`${BASE_URL}/api/v1/messages`, async (route) => {
      const response = await route.fetch();
      statuses.push(response.status());
      await route.fulfill({ response });
    });
    await page.getByRole('button', { name: 'Submit form' }).click();
    await expect.poll(() => statuses).toEqual([403]);
  },
);

test(
  'a cross-origin opener loses its reference to the isolated application',
  { tag: '@cross-browser' },
  async ({ page }) => {
    await page.route(`${POW_BASE_URL}/browser-opener`, (route) =>
      route.fulfill({
        contentType: 'text/html',
        body: `<a href="${BASE_URL}/" target="_blank" rel="opener">Open Nylzo</a>`,
      }),
    );
    await page.goto(`${POW_BASE_URL}/browser-opener`);
    const opened = page.waitForEvent('popup');
    await page.getByRole('link', { name: 'Open Nylzo' }).click();
    const popup = await opened;
    await popup.locator('html[data-ready]').waitFor({ state: 'attached' });
    expect(await popup.evaluate(() => window.opener === null)).toBe(true);
    await popup.close();
  },
);

test(
  'message text is never parsed as HTML or handed to a markup sanitizer',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const text =
      '<svg onload="globalThis.nylzoXss=true"></svg><img src=x onerror="globalThis.nylzoXss=true"><script>globalThis.nylzoXss=true</script><b>keep these tags as text</b>';
    const message = await createMessageThroughApi({ text });
    await showMessage(page, message.link);
    await expectMessageShown(page);
    await expect(page.locator('pre')).toHaveText(text);
    await expect(page.locator('pre').locator('svg, img, script, b')).toHaveCount(0);
    expect(await page.evaluate(() => Reflect.get(globalThis, 'nylzoXss') as unknown)).toBeUndefined();
  },
);

test("native Trusted Types reject string sinks, unapproved policies and the app's own policy names, and CSP rejects eval (W2-10)", async ({
  page,
}) => {
  await openApp(page, '/');
  const outcome = await page.evaluate(() => {
    const element = document.createElement('div');
    document.body.append(element);
    const policy = Reflect.get(globalThis, 'trustedTypes') as {
      defaultPolicy: unknown;
      createPolicy: (name: string, rules: { createHTML: (input: string) => string }) => unknown;
    };
    // eslint-disable-next-line unicorn/consistent-function-scoping -- Playwright serializes this callback, so its helper must stay inside it.
    function blocked(action: () => unknown): string {
      try {
        action();
        return 'allowed';
      } catch (error) {
        return error instanceof Error ? error.name : 'unknown';
      }
    }
    const html = '<img src=x onerror="globalThis.nylzoXss=true">';
    const result = {
      inner: blocked(() => Reflect.set(element, 'innerHTML', html)),
      outer: blocked(() => Reflect.set(element, 'outerHTML', html)),
      write: blocked(() => {
        // eslint-disable-next-line no-restricted-properties, @typescript-eslint/no-deprecated -- Tests a forbidden legacy sink under native Trusted Types.
        document.write(html);
      }),
      policy: blocked(() => policy.createPolicy('unapproved', { createHTML: (input) => input })),
      kdfPolicy: blocked(() => policy.createPolicy('nylzo-kdf', { createHTML: (input) => input })),
      sveltePolicy: blocked(() => policy.createPolicy('svelte-trusted-html', { createHTML: (input) => input })),
      defaultPolicy: policy.defaultPolicy,
    };
    element.remove();
    const button = document.createElement('button');
    button.textContent = 'Probe eval';
    button.addEventListener('click', () => {
      const evaluate = Reflect.get(globalThis, 'eval') as (source: string) => unknown;
      button.dataset['result'] = blocked(() => evaluate('globalThis.nylzoXss=true'));
    });
    document.body.append(button);
    return result;
  });
  expect(outcome).toMatchObject({
    inner: 'TypeError',
    outer: 'TypeError',
    write: 'TypeError',
    policy: 'TypeError',
    kdfPolicy: 'TypeError',
    sveltePolicy: 'TypeError',
    defaultPolicy: null,
  });
  const probe = page.getByRole('button', { name: 'Probe eval' });
  await probe.click();
  await expect(probe).toHaveAttribute('data-result', /^(?:TypeError|EvalError)$/);
  expect(await page.evaluate(() => Reflect.get(globalThis, 'nylzoXss') as unknown)).toBeUndefined();
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
});

test(
  'the entry module claims both Trusted Types policy names before the rest of the client loads (W2-10)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createMessageThroughApi();
    const held: string[] = [];
    await page.route(/\/assets\/start-[\w-]+\.js$/, (route) => {
      held.push(route.request().url());
    });
    await page.goto(message.link, { waitUntil: 'commit' });
    await expect.poll(() => held.length).toBe(1);
    const claimed = await page.evaluate(() => {
      const factory = Reflect.get(globalThis, 'trustedTypes') as {
        createPolicy: (name: string, rules: Record<string, never>) => unknown;
      };
      return ['nylzo-kdf', 'svelte-trusted-html'].map((name) => {
        try {
          factory.createPolicy(name, {});
          return `${name} free`;
        } catch {
          return `${name} taken`;
        }
      });
    });
    expect(claimed).toEqual(['nylzo-kdf taken', 'svelte-trusted-html taken']);
    await page.unrouteAll({ behavior: 'ignoreErrors' });
  },
);
