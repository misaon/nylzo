import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Live, Overview, UsagePoint } from '../../api/types.ts';
import { adminTranslator } from '../../messages/catalog.ts';
import { getDashboardTools } from '../context.ts';
import { dashboardFormat } from '../format.ts';
import { EMPTY_LIVE, emptyOverview } from '../overview.ts';
import NowStrip from './NowStrip.svelte';

vi.mock(import('../context.ts'), (original) =>
  original().then((actual) => ({ ...actual, getDashboardTools: vi.fn<typeof actual.getDashboardTools>() })),
);

const NOW = Date.UTC(2026, 10, 1, 12) / 1000;
const GIBIBYTE = 1024 ** 3;

function sample(at: number, overrides: Partial<UsagePoint> = {}): UsagePoint {
  return {
    at,
    processor: 0.5,
    load: 0.2,
    memoryUsed: GIBIBYTE,
    memoryTotal: 4 * GIBIBYTE,
    processBytes: 1,
    ...overrides,
  };
}

interface StripProps {
  readonly live: Live;
  readonly overview: Overview;
  readonly waiting: boolean;
  readonly loading: boolean;
  readonly stale: boolean;
}

describe('now strip', () => {
  const { translate } = adminTranslator('en');
  const format = dashboardFormat('en');
  let target: HTMLElement;
  let component: ReturnType<typeof mount> | undefined;

  function show(props: StripProps): void {
    component = mount(NowStrip, { target, props });
    flushSync();
  }

  function tile(label: string): string {
    const heading = [...target.querySelectorAll('p')].find((element) => element.textContent.trim() === label);
    return (
      heading
        ?.closest('.rounded-pane')
        ?.textContent.replaceAll(/[\t\n ]+/gu, ' ')
        .trim() ?? ''
    );
  }

  function online(): Element | null | undefined {
    return [...target.querySelectorAll('p')].find(
      (element) => element.textContent.trim() === translate('dashboard.now.online'),
    )?.nextElementSibling;
  }

  function badge(): string {
    return target.querySelector('h2 + span')?.textContent.trim() ?? '';
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
  });

  it('shows the latest processor, memory and disk use with the people online', () => {
    show({
      live: {
        ...EMPTY_LIVE,
        online: 7,
        people: [1, 2, 3],
        robots: [4, 5],
        usage: [sample(NOW - 5, { processor: -1, memoryTotal: 0 }), sample(NOW, { processor: 0.42 })],
      },
      overview: {
        ...emptyOverview('30d', NOW),
        stored: 12,
        storage: { freeBytes: 40 * GIBIBYTE, totalBytes: 100 * GIBIBYTE, reserveBytes: 5 * GIBIBYTE, databaseBytes: 0 },
      },
      waiting: false,
      loading: false,
      stale: false,
    });

    expect(badge()).toBe(translate('dashboard.now.live'));
    expect(target.textContent).toContain(`${translate('dashboard.now.people')} ${format.count(6)}`);
    expect(target.textContent).toContain(`${translate('dashboard.now.robots')} ${format.count(9)}`);
    expect(online()?.querySelector('.sr-only')?.textContent).toBe(format.count(7));
    expect(tile(translate('dashboard.now.processor'))).toContain(format.percent(0.42));
    expect(tile(translate('dashboard.now.processor'))).toContain(translate('dashboard.now.five'));
    expect(tile(translate('dashboard.now.memory'))).toContain(format.percent(0.25));
    expect(tile(translate('dashboard.now.memory'))).toContain(
      translate('dashboard.now.memory.value', { used: format.bytes(GIBIBYTE), total: format.bytes(4 * GIBIBYTE) }),
    );
    expect(tile(translate('dashboard.now.disk'))).toContain(format.bytes(40 * GIBIBYTE));
    expect(tile(translate('dashboard.now.disk'))).toContain(
      translate('dashboard.now.disk.reserve', { reserve: format.bytes(5 * GIBIBYTE) }),
    );
    expect(tile(translate('dashboard.now.stored'))).toContain(format.count(12));
  });

  it('holds placeholders while the first figures load', () => {
    show({ live: EMPTY_LIVE, overview: emptyOverview('30d', NOW), waiting: true, loading: true, stale: false });

    expect(online()?.textContent.trim()).toBe('');
    expect(target.textContent).toContain(`${translate('dashboard.now.people')} –`);
    for (const label of ['dashboard.now.processor', 'dashboard.now.memory', 'dashboard.now.disk'] as const) {
      expect(tile(translate(label))).toContain(`– ${translate('dashboard.now.waiting')}`);
    }
    expect(tile(translate('dashboard.now.stored'))).not.toMatch(/\d/u);
  });

  it('says when the server cannot measure its use', () => {
    show({
      live: { ...EMPTY_LIVE, usageSupported: false, usage: [sample(NOW)] },
      overview: emptyOverview('30d', NOW),
      waiting: false,
      loading: false,
      stale: false,
    });

    expect(tile(translate('dashboard.now.processor'))).toContain(translate('dashboard.now.unavailable'));
    expect(tile(translate('dashboard.now.memory'))).toContain(translate('dashboard.now.unavailable'));
  });

  it('shows when the live figures stopped and since when', () => {
    show({
      live: { ...EMPTY_LIVE, usage: [sample(NOW)] },
      overview: emptyOverview('30d', NOW),
      waiting: false,
      loading: false,
      stale: true,
    });
    expect(badge()).toBe(translate('dashboard.now.stale', { time: format.clock(NOW) }));
  });

  it('shows the live figures as paused before any sample arrived', () => {
    show({ live: EMPTY_LIVE, overview: emptyOverview('30d', NOW), waiting: false, loading: false, stale: true });
    expect(badge()).toBe(translate('dashboard.now.paused'));
  });
});
