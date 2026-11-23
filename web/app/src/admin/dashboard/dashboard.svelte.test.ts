import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Live, Overview, Range } from '../api/types.ts';
import { adminTranslator } from '../messages/catalog.ts';
import { getAdminTranslator } from '../messages/translator-context.ts';
import Dashboard from './Dashboard.svelte';
import { EMPTY_LIVE, emptyOverview } from './overview.ts';

vi.mock(import('../messages/translator-context.ts'), (original) =>
  original().then((actual) => ({ ...actual, getAdminTranslator: vi.fn<typeof actual.getAdminTranslator>() })),
);

const NOW = Date.UTC(2026, 10, 1, 12) / 1000;

interface DashboardProps {
  overview: Overview | undefined;
  live: Live | undefined;
  range: Range;
  failed: boolean;
  stale: boolean;
}

describe('dashboard', () => {
  const { translate } = adminTranslator('en');
  const onrange = vi.fn<(range: Range) => void>();
  const props = $state<DashboardProps>({
    overview: undefined,
    live: undefined,
    range: '30d',
    failed: false,
    stale: false,
  });
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;

  function status(): string | undefined {
    return target.querySelector('[role="status"]')?.textContent.trim();
  }

  function busy(): string | null | undefined {
    return target.querySelector('[aria-busy]')?.getAttribute('aria-busy');
  }

  function rangeButton(range: Range): HTMLButtonElement {
    const button = [...target.querySelectorAll('button[aria-pressed]')].find(
      (candidate) => candidate.textContent.trim() === translate(`dashboard.range.${range}`),
    );
    if (!(button instanceof HTMLButtonElement)) {
      throw new TypeError(`missing range ${range}`);
    }
    return button;
  }

  beforeEach(() => {
    vi.useFakeTimers({ now: NOW * 1000, toFake: ['Date'] });
    vi.mocked(getAdminTranslator).mockReturnValue(adminTranslator('en'));
    onrange.mockReset();
    Object.assign(props, { overview: undefined, live: undefined, range: '30d', failed: false, stale: false });
    target = document.createElement('div');
    document.body.append(target);
    component = mount(Dashboard, {
      target,
      props: {
        get overview() {
          return props.overview;
        },
        get live() {
          return props.live;
        },
        get range() {
          return props.range;
        },
        get failed() {
          return props.failed;
        },
        get stale() {
          return props.stale;
        },
        onrange,
      },
    });
    flushSync();
  });
  afterEach(async () => {
    await unmount(component);
    target.remove();
    vi.useRealTimers();
  });

  it('focuses its heading and announces that the figures are loading', () => {
    expect(document.activeElement).toBe(target.querySelector('h1'));
    expect(status()).toBe(translate('dashboard.loading'));
    expect(busy()).toBe('true');
    expect(target.textContent).toContain(translate('dashboard.range.long.30d'));
  });

  it('tells apart a first load that failed from a refresh that failed', () => {
    props.failed = true;
    flushSync();
    expect(status()).toBe(translate('dashboard.failed.first'));

    props.overview = emptyOverview('30d', NOW);
    flushSync();
    expect(status()).toBe(translate('dashboard.failed'));

    props.failed = false;
    flushSync();
    expect(status()).toBeUndefined();
  });

  it('stays busy while the figures of another range load', () => {
    props.overview = emptyOverview('30d', NOW);
    props.live = EMPTY_LIVE;
    flushSync();
    expect(busy()).toBe('false');

    props.range = '7d';
    flushSync();
    expect(busy()).toBe('true');
    expect(target.textContent).toContain(translate('dashboard.range.long.30d'));

    props.overview = emptyOverview('7d', NOW);
    flushSync();
    expect(busy()).toBe('false');
    expect(target.textContent).toContain(translate('dashboard.range.long.7d'));
  });

  it('marks the chosen range and asks for another one', () => {
    expect(rangeButton('30d').getAttribute('aria-pressed')).toBe('true');
    expect(rangeButton('90d').getAttribute('aria-pressed')).toBe('false');

    rangeButton('90d').click();
    expect(onrange).toHaveBeenCalledExactlyOnceWith('90d');
  });

  function marker(): string {
    return target.querySelector('[role="group"] > span')?.getAttribute('class') ?? '';
  }

  it('slides the range marker to the chosen range', () => {
    const positions = (['24h', '7d', '30d', '90d', '12m'] as const).map((range) => {
      props.range = range;
      flushSync();
      return /translate-x-\S+/u.exec(marker())?.[0];
    });
    expect(new Set(positions).size).toBe(5);
  });
});
