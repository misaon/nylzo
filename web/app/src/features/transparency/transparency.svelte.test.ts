import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Transparency as TransparencyData } from '../../shared/api/api.ts';
import { createApi } from '../../shared/api/api.ts';
import { fakeApi } from '../../shared/api/fake-api.ts';
import { getTranslator, Translator } from '../../shared/language/translator.svelte.ts';
import { instanceOrigin, OFFICIAL_ORIGIN } from '../../shared/local-client/instance-origin.ts';
import Transparency from './Transparency.svelte';
import { contactLabel } from './transparency.ts';

vi.mock('../../shared/api/api.ts', () => ({ createApi: vi.fn<typeof import('../../shared/api/api.ts').createApi>() }));
vi.mock(import('../../shared/local-client/instance-origin.ts'), (original) =>
  original().then((actual) => ({ ...actual, instanceOrigin: vi.fn<typeof actual.instanceOrigin>() })),
);
vi.mock(import('../../shared/language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
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
    vi.mocked(instanceOrigin).mockReturnValue('https://self-hosted.example.test');
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
        coarseMultiplier: 2,
        globalDailyCreates: 0,
        powDifficulty: 4,
      },
      retention: { tombstoneSeconds: 86_400, dailyCounterSeconds: 86_400 },
      visits: { counted: false, hourlySeconds: 0, dailySeconds: 0 },
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
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    location.hash = '';
  });

  function listed(section: string): number {
    return target.querySelectorAll(`[aria-labelledby="${section}"] :is(dt, h3)`).length;
  }

  it.each([
    { origin: OFFICIAL_ORIGIN, checks: 5, others: 3, personal: 4 },
    { origin: 'https://self-hosted.example.test', checks: 4, others: 2, personal: 3 },
  ])(
    'lists the checks, other parties and personal data roles that apply to $origin',
    async ({ origin, checks, others, personal }) => {
      vi.mocked(instanceOrigin).mockReturnValue(origin);
      show();
      await vi.waitFor(() => expect(target.querySelector('[data-testid="server-version"]')).not.toBeNull());
      expect([listed('transparency-checks'), listed('transparency-others'), listed('transparency-personal')]).toEqual([
        checks,
        others,
        personal,
      ]);
      const writeText = vi.fn<(text: string) => Promise<void>>().mockResolvedValue();
      vi.stubGlobal('navigator', { clipboard: { writeText } });
      target.querySelector<HTMLButtonElement>(`button[aria-label="${translator.translate('action.copy')}"]`)?.click();
      expect(writeText).toHaveBeenCalledExactlyOnceWith(`nylzo verify-client --server ${origin}`);
    },
  );

  it('lists counted visits among what the server stores only when the server counts them', async () => {
    load.mockResolvedValue({ ...data, visits: { counted: true, hourlySeconds: 86_400, dailySeconds: 86_400 } });
    show();
    await vi.waitFor(() => expect(target.querySelector('[data-testid="server-version"]')).not.toBeNull());
    const stores = listed('transparency-stores');
    await unmount(component);
    load.mockResolvedValue(data);
    show();
    await vi.waitFor(() => expect(target.querySelector('[data-testid="server-version"]')).not.toBeNull());
    expect(listed('transparency-stores')).toBe(stores - 1);
  });

  it('scrolls to the section the link names once the sections exist', async () => {
    const scrolled = vi.spyOn(Element.prototype, 'scrollIntoView').mockReturnValue();
    location.hash = '#transparency-limits';
    show();
    await vi.waitFor(() => expect(scrolled).toHaveBeenCalledOnce());
    expect(scrolled.mock.contexts[0]).toBe(target.querySelector('#transparency-limits'));
  });

  it.each([
    { operator: { hosting: 'Synthetic hosting' }, shown: ['Synthetic hosting'] },
    { operator: { name: 'Synthetic operator' }, shown: ['Synthetic operator'] },
    { operator: {}, shown: [translator.translate('transparency.operator.missing')] },
  ])('shows only the operator details the server publishes: %j', async ({ operator, shown }) => {
    load.mockResolvedValue({ ...data, operator });
    show();
    await vi.waitFor(() => expect(target.querySelector('[data-testid="server-version"]')).not.toBeNull());
    const details = [
      ...target.querySelectorAll(
        '[aria-labelledby="transparency-operator"] dd, [aria-labelledby="transparency-operator"] p',
      ),
    ];
    expect(details.map((detail) => detail.textContent.trim())).toEqual(shown);
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
    expect(target.textContent).not.toContain(translator.translate('transparency.loading'));
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
      expect(target.textContent).toContain(
        operator === undefined ? translator.translate('transparency.operator.missing') : contactLabel(operator.contact),
      );
      expect(target.querySelector('a[href="https://example.test/contact"]') !== null).toBe(
        operator?.contact === 'https://example.test/contact',
      );
      expect(target.querySelector('a[href^="javascript:"]')).toBeNull();
      expect(target.textContent).toContain(translator.translate('transparency.limits.none'));
      expect(target.textContent).toContain(translator.translate('transparency.limits.global.value', { max: 5 }));
    },
  );
});
