import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Live, Overview, RefusedClient } from '../../api/types.ts';
import { byTimelineMetric } from '../../api/types.ts';
import { adminTranslator } from '../../messages/catalog.ts';
import { getDashboardTools } from '../context.ts';
import { dashboardFormat } from '../format.ts';
import { EMPTY_LIVE, emptyOverview } from '../overview.ts';
import ProtectionSection from './ProtectionSection.svelte';

vi.mock(import('../context.ts'), (original) =>
  original().then((actual) => ({ ...actual, getDashboardTools: vi.fn<typeof actual.getDashboardTools>() })),
);

const NOW = Date.UTC(2026, 10, 1, 12) / 1000;

function client(overrides: Partial<RefusedClient>): RefusedClient {
  return { client: '192.0.2.1', country: '', reasons: {}, count: 1, first: NOW - 600, last: NOW - 60, ...overrides };
}

function limits(row: Element | undefined): string[] {
  return [...(row?.querySelectorAll('ul li') ?? [])].map((reason) =>
    reason.textContent.replaceAll(/[\t\n ]+/gu, ' ').trim(),
  );
}

interface SectionProps {
  readonly overview: Overview;
  readonly live: Live;
  readonly loading: boolean;
  readonly waiting: boolean;
}

describe('protection section', () => {
  const { translate } = adminTranslator('en');
  const format = dashboardFormat('en');
  let target: HTMLElement;
  let component: ReturnType<typeof mount> | undefined;

  const props = $state<{ live: Live }>({ live: EMPTY_LIVE });

  function show({ overview, live, loading, waiting }: SectionProps): void {
    props.live = live;
    component = mount(ProtectionSection, {
      target,
      props: {
        overview,
        get live() {
          return props.live;
        },
        range: overview.range,
        loading,
        waiting,
        now: NOW,
      },
    });
    flushSync();
  }

  function addresses(): string[] {
    return [...target.querySelectorAll('[translate="no"]')].map((address) => address.textContent.trim());
  }

  beforeEach(() => {
    vi.mocked(getDashboardTools).mockReturnValue({ translate, format });
    target = document.createElement('div');
    document.body.append(target);
  });
  afterEach(() => {
    if (component) {
      void unmount(component);
    }
    target.remove();
    vi.restoreAllMocks();
  });

  it('counts the refusals and compares them with the period before', () => {
    show({
      overview: {
        ...emptyOverview('30d', NOW),
        totals: byTimelineMetric((metric) => (metric === 'refusals' ? 30 : 0)),
        previous: byTimelineMetric((metric) => (metric === 'refusals' ? 20 : 0)),
      },
      live: EMPTY_LIVE,
      loading: false,
      waiting: false,
    });

    const stat = target.querySelector('dd')?.textContent ?? '';
    expect(stat).toContain(format.count(30));
    expect(stat).toContain(format.change(0.5));
    expect(target.textContent).toContain(translate('dashboard.protection.empty'));
  });

  it('holds the list of clients while it waits for the live figures', () => {
    show({ overview: emptyOverview('30d', NOW), live: EMPTY_LIVE, loading: true, waiting: true });

    expect(target.querySelector('[aria-hidden="true"].grid .animate-pulse')).not.toBeNull();
    expect(target.textContent).not.toContain(translate('dashboard.protection.empty'));
    expect(addresses()).toEqual([]);
  });

  it('lists each refused client with its country, the limits that stopped it and when', () => {
    show({
      overview: emptyOverview('30d', NOW),
      live: {
        ...EMPTY_LIVE,
        refused: [
          client({
            client: '192.0.2.1',
            country: 'DE',
            reasons: { read: 2, create: 5, admin: 0, 'future-limit': 1 },
            count: 8,
            first: NOW - 3600,
            last: NOW + 30,
          }),
          client({ client: '2001:db8::/64', country: '', reasons: { requests: 1 }, count: 1 }),
        ],
      },
      loading: false,
      waiting: false,
    });

    const [first, second] = [...target.querySelectorAll(':scope ul[role="list"] > li:has(> div)')];
    expect(first?.textContent).toContain('192.0.2.1');
    expect(first?.textContent).toContain(format.country('DE'));
    expect(limits(first)).toEqual([
      `${translate('dashboard.limit.create')} ${format.count(5)}`,
      `${translate('dashboard.limit.read')} ${format.count(2)}`,
      `future-limit ${format.count(1)}`,
    ]);
    expect([...(first?.querySelectorAll('time') ?? [])].map((time) => time.textContent.trim())).toEqual([
      format.relative(NOW - 3600, NOW),
      format.relative(NOW, NOW),
    ]);
    expect(second?.textContent).toContain(translate('dashboard.where.unknown'));
    expect(limits(second)).toEqual([`${translate('dashboard.limit.requests')} ${format.count(1)}`]);
  });

  it('moves a client to the top when a limit refuses it again', () => {
    const first = client({ client: '192.0.2.1' });
    const second = client({ client: '192.0.2.2' });
    show({
      overview: emptyOverview('30d', NOW),
      live: { ...EMPTY_LIVE, refused: [first, second] },
      loading: false,
      waiting: false,
    });
    expect(addresses()).toEqual(['192.0.2.1', '192.0.2.2']);
    const animate = vi.spyOn(Element.prototype, 'animate');
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function place(this: Element) {
      const row = [...(this.parentElement?.children ?? [])].indexOf(this);
      return new DOMRect(0, row * 48, 640, 48);
    });

    props.live = { ...EMPTY_LIVE, refused: [{ ...second, count: 2, last: NOW }, first] };
    flushSync();
    expect(addresses()).toEqual(['192.0.2.2', '192.0.2.1']);
    expect(animate).toHaveBeenCalledTimes(2);
  });
});
