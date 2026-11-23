import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Live, Overview, UsagePoint } from '../../api/types.ts';
import { adminTranslator } from '../../messages/catalog.ts';
import { getDashboardTools } from '../context.ts';
import { dashboardFormat } from '../format.ts';
import { EMPTY_LIVE, emptyOverview } from '../overview.ts';
import ServerSection from './ServerSection.svelte';

vi.mock(import('../context.ts'), (original) =>
  original().then((actual) => ({ ...actual, getDashboardTools: vi.fn<typeof actual.getDashboardTools>() })),
);

const NOW = Date.UTC(2026, 10, 1, 12) / 1000;
const MEBIBYTE = 1024 ** 2;
const GIBIBYTE = 1024 ** 3;

function sample(at: number, overrides: Partial<UsagePoint> = {}): UsagePoint {
  return {
    at,
    processor: 0.5,
    load: 0.37,
    memoryUsed: GIBIBYTE,
    memoryTotal: 4 * GIBIBYTE,
    processBytes: 300 * MEBIBYTE,
    ...overrides,
  };
}

interface SectionProps {
  live: Live;
  overview: Overview;
  loading: boolean;
  waiting: boolean;
}

describe('server section', () => {
  const { translate } = adminTranslator('en');
  const format = dashboardFormat('en');
  const props = $state<SectionProps>({
    live: EMPTY_LIVE,
    overview: emptyOverview('30d', NOW),
    loading: false,
    waiting: false,
  });
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;

  function card(title: string): string {
    const heading = [...target.querySelectorAll('h3')].find((element) => element.textContent.trim() === title);
    return (
      heading
        ?.closest('.glass')
        ?.textContent.replaceAll(/[\t\n ]+/gu, ' ')
        .trim() ?? ''
    );
  }

  function figure(label: string): string {
    const term = [...target.querySelectorAll('dt')].find((element) => element.textContent.trim() === label);
    return term?.nextElementSibling?.textContent.replaceAll(/[\t\n ]+/gu, ' ').trim() ?? '';
  }

  function pulses(): number {
    return target.querySelectorAll('dd .animate-pulse').length;
  }

  beforeEach(() => {
    vi.mocked(getDashboardTools).mockReturnValue({ translate, format });
    Object.assign(props, { live: EMPTY_LIVE, overview: emptyOverview('30d', NOW), loading: false, waiting: false });
    target = document.createElement('div');
    document.body.append(target);
    component = mount(ServerSection, {
      target,
      props: {
        get live() {
          return props.live;
        },
        get overview() {
          return props.overview;
        },
        get loading() {
          return props.loading;
        },
        get waiting() {
          return props.waiting;
        },
      },
    });
    flushSync();
  });
  afterEach(async () => {
    await unmount(component);
    target.remove();
  });

  it('charts the processor and memory use and lists the size of the process, disk and database', () => {
    props.live = { ...EMPTY_LIVE, usage: [sample(NOW - 10), sample(NOW - 5, { processor: -1 }), sample(NOW)] };
    props.overview = {
      ...emptyOverview('30d', NOW),
      storage: {
        freeBytes: 40 * GIBIBYTE,
        totalBytes: 100 * GIBIBYTE,
        reserveBytes: 5 * GIBIBYTE,
        databaseBytes: 12 * MEBIBYTE,
      },
      version: 'v0.14.0',
    };
    flushSync();

    const processor = card(translate('dashboard.server.processor'));
    expect(processor).toContain(format.percent(0.5));
    expect(processor).toContain(translate('dashboard.server.load', { load: format.decimal(0.37) }));
    expect(target.querySelectorAll('[role="slider"]')).toHaveLength(2);
    const memory = card(translate('dashboard.server.memory'));
    expect(memory).toContain(format.percent(0.25));
    expect(memory).toContain(
      translate('dashboard.now.memory.value', { used: format.bytes(GIBIBYTE), total: format.bytes(4 * GIBIBYTE) }),
    );
    expect(figure(translate('dashboard.server.process'))).toBe(format.bytes(300 * MEBIBYTE));
    expect(figure(translate('dashboard.server.disk'))).toBe(
      `${format.bytes(40 * GIBIBYTE)} ${translate('dashboard.now.disk.value', { total: format.bytes(100 * GIBIBYTE) })} ${translate('dashboard.now.disk.reserve', { reserve: format.bytes(5 * GIBIBYTE) })}`,
    );
    expect(figure(translate('dashboard.server.database'))).toBe(format.bytes(12 * MEBIBYTE));
    expect(figure(translate('dashboard.server.version'))).toBe('v0.14.0');
  });

  it('waits for the first samples and figures', () => {
    props.waiting = true;
    props.loading = true;
    flushSync();

    expect(card(translate('dashboard.server.processor'))).toContain(translate('dashboard.now.waiting'));
    expect(card(translate('dashboard.server.memory'))).toContain(translate('dashboard.now.waiting'));
    expect(target.querySelectorAll('[role="slider"]')).toHaveLength(0);
    expect(pulses()).toBe(4);
    expect(target.querySelectorAll('dd svg')).toHaveLength(1);
  });

  it('calls what the server cannot measure unknown', () => {
    props.live = {
      ...EMPTY_LIVE,
      usage: [sample(NOW, { processor: -1, load: -1, memoryTotal: 0, processBytes: 0 })],
    };
    flushSync();

    const processor = card(translate('dashboard.server.processor'));
    expect(processor).toContain(`– ${translate('dashboard.now.five')}`);
    expect(processor).toContain(translate('dashboard.server.unavailable'));
    expect(card(translate('dashboard.server.memory'))).toContain(`– ${translate('dashboard.now.five')}`);
    const unknown = translate('dashboard.unknown');
    for (const label of ['process', 'disk', 'database', 'version'] as const) {
      expect(figure(translate(`dashboard.server.${label}`))).toBe(unknown);
    }
    expect(target.querySelectorAll('dd svg')).toHaveLength(0);
    expect(pulses()).toBe(0);
  });

  it('labels the moment under the pointer of a chart and keeps doing so when samples drop out', () => {
    props.live = { ...EMPTY_LIVE, usage: [sample(NOW - 10), sample(NOW - 5), sample(NOW)] };
    flushSync();
    const slider = target.querySelector<HTMLElement>('[role="slider"]');
    slider?.dispatchEvent(new FocusEvent('focus'));
    flushSync();
    expect(slider?.getAttribute('aria-valuetext')).toContain(format.time(NOW));

    props.live = { ...EMPTY_LIVE, usage: [sample(NOW)] };
    flushSync();
    expect(slider?.getAttribute('aria-valuetext')).toContain(format.time(NOW));
  });
});
