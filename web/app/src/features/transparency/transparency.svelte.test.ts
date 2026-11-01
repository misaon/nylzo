// @vitest-environment happy-dom
import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Transparency as TransparencyData } from '../../shared/api/api.ts';

import { createApi } from '../../shared/api/api.ts';
import { fakeApi } from '../../shared/api/fake-api.ts';
import { getTranslator, Translator } from '../../shared/language/translator.svelte.ts';
import Transparency from './Transparency.svelte';

vi.mock('../../shared/api/api.ts', () => ({ createApi: vi.fn() }));
vi.mock(import('../../shared/language/translator.svelte.ts'), (original) =>
  // eslint-disable-next-line unicorn/prefer-await -- Svelte instruments awaits before hoisted imports initialize.
  original().then((actual) => ({ ...actual, getTranslator: vi.fn() })),
);

describe('transparency page', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;
  let data: TransparencyData;
  const load = vi.fn<() => Promise<TransparencyData>>();
  const translator = new Translator('en');

  function show(): void {
    component = mount(Transparency, { target });
    flushSync();
  }

  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(getTranslator).mockReturnValue(translator);
    const api = fakeApi();
    data = {
      server: 'synthetic-build',
      operator: undefined,
      limits: {
        maxFileBytes: 16,
        maxTextBytes: 102_400,
        maxObjectBytes: 5_373_952,
        maxTtlSeconds: 86_400,
        defaultTtlSeconds: 3600,
        maxOpens: 3,
      },
      rates: {
        createPerHour: 10,
        createPerDay: 20,
        createBytesPerDay: 1024,
        readPerHour: 30,
        abusePerHour: 5,
        coarseMultiplier: 2,
        globalDailyCreates: 0,
        powDifficulty: 4,
      },
      retention: { tombstoneSeconds: 86_400, abuseReportSeconds: 172_800, dailyCounterSeconds: 86_400 },
      files: [],
    };
    api.transparency = load;
    vi.mocked(createApi).mockReturnValue(api);
    load.mockResolvedValue(data);
    target = document.createElement('div');
    document.body.append(target);
  });
  afterEach(async () => {
    await unmount(component);
    target.remove();
  });

  it('shows loading, then publishes server limits, retention and verified asset hashes', async () => {
    const gate = Promise.withResolvers<TransparencyData>();
    load.mockReturnValue(gate.promise);
    show();
    expect(target.querySelector('[role="status"]')?.textContent).toBe(translator.translate('transparency.loading'));
    gate.resolve({
      ...data,
      operator: { name: 'Synthetic operator', contact: 'abuse@example.test', hosting: 'Synthetic hosting' },
      files: [{ path: '/assets/client.js', sha256: 'a'.repeat(64) }],
    });
    await vi.waitFor(() => expect(target.textContent).toContain('Synthetic operator'));
    expect(target.textContent).toContain('Synthetic hosting');
    expect(target.querySelector('a[href="mailto:abuse@example.test"]')?.textContent).toBe('abuse@example.test');
    expect(target.textContent).toContain('/assets/client.js');
    expect(target.textContent).toContain('a'.repeat(64));
    expect(target.querySelector('[data-testid="server-version"]')?.textContent).toBe(data.server);
    expect(target.textContent).toContain('nylzo verify-client --server');
    expect(target.querySelector('[role="status"]')).toBeNull();
  });

  it('offers retry after failure and recovers without reloading the document', async () => {
    load.mockRejectedValueOnce(new Error('offline'));
    show();
    await vi.waitFor(() =>
      expect(target.querySelector('[role="alert"]')?.textContent).toBe(translator.translate('transparency.failed')),
    );
    target.querySelector('button')?.click();
    await vi.waitFor(() =>
      expect(target.querySelector('[data-testid="server-version"]')?.textContent).toBe(data.server),
    );
    expect(load).toHaveBeenCalledTimes(2);
    expect(target.querySelector('[role="alert"]')).toBeNull();
  });

  it.each([undefined, { contact: 'javascript:alert(1)' }, { contact: 'https://example.test/contact' }])(
    'renders optional operator information without unsafe links: %j',
    async (operator) => {
      load.mockResolvedValue({
        ...data,
        operator,
        limits: { ...data.limits, maxTtlSeconds: 3600 },
        rates: { ...data.rates, createPerHour: 0, createPerDay: 0, readPerHour: 0, globalDailyCreates: 5 },
      });
      show();
      await vi.waitFor(() => expect(target.querySelector('[data-testid="server-version"]')).not.toBeNull());
      expect(target.textContent).toContain(operator?.contact ?? translator.translate('transparency.operator.missing'));
      expect(target.querySelector('a[href="https://example.test/contact"]') !== null).toBe(
        operator?.contact === 'https://example.test/contact',
      );
      expect(target.querySelector('a[href^="javascript:"]')).toBeNull();
      expect(target.textContent).toContain(translator.translate('transparency.limits.none'));
      expect(target.textContent).toContain(translator.translate('transparency.limits.global.value', { max: 5 }));
    },
  );
});
