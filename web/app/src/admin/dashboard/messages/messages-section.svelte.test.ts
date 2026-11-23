import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Overview } from '../../api/types.ts';
import { byTimelineMetric } from '../../api/types.ts';
import { adminTranslator } from '../../messages/catalog.ts';
import { getDashboardTools } from '../context.ts';
import { dashboardFormat } from '../format.ts';
import { emptyOverview } from '../overview.ts';
import MessagesSection from './MessagesSection.svelte';

vi.mock(import('../context.ts'), (original) =>
  original().then((actual) => ({ ...actual, getDashboardTools: vi.fn<typeof actual.getDashboardTools>() })),
);

const NOW = Date.UTC(2026, 10, 1, 12) / 1000;

describe('messages section', () => {
  const { translate } = adminTranslator('en');
  const format = dashboardFormat('en');
  let target: HTMLElement;
  let component: ReturnType<typeof mount> | undefined;

  function show(overview: Overview, loading = false): void {
    component = mount(MessagesSection, { target, props: { overview, range: overview.range, loading } });
    flushSync();
  }

  function stat(label: string): string {
    const term = [...target.querySelectorAll('dt')].find((element) => element.textContent.trim() === label);
    return term?.nextElementSibling?.textContent.replaceAll(/[\t\n ]+/gu, ' ').trim() ?? '';
  }

  function trendTone(label: string): string {
    const term = [...target.querySelectorAll('dt')].find((element) => element.textContent.trim() === label);
    return term?.nextElementSibling?.querySelector('p > span')?.getAttribute('class') ?? '';
  }

  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(800);
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

  it('counts each message event, compares it with the period before and colours only the good growth', () => {
    show({
      ...emptyOverview('30d', NOW),
      totals: byTimelineMetric((metric) => (metric.startsWith('messages.') ? 12 : 0)),
      previous: byTimelineMetric((metric) => (metric === 'messages.opened' ? 16 : 8)),
      stored: 4,
    });

    expect(stat(translate('dashboard.messages.created'))).toContain(format.change(0.5));
    expect(trendTone(translate('dashboard.messages.created'))).toContain('text-aegis-200');
    expect(stat(translate('dashboard.messages.opened'))).toContain(format.change(-0.25));
    expect(trendTone(translate('dashboard.messages.opened'))).toContain('text-rose-200');
    expect(trendTone(translate('dashboard.messages.expired'))).toContain('text-ink-soft');
    expect(stat(translate('dashboard.messages.stored'))).toBe(`${format.count(4)}${format.count(4)}`);
    expect(target.textContent).not.toContain(translate('dashboard.empty'));
    expect(target.querySelector('[role="slider"]')?.getAttribute('aria-valuemax')).toBe('29');
  });

  it('says when no message moved and leaves out comparisons the server does not keep', () => {
    show(emptyOverview('12m', NOW));

    expect(target.textContent).toContain(translate('dashboard.empty'));
    expect(stat(translate('dashboard.messages.created'))).not.toContain('%');
  });

  it('holds the figures while they load', () => {
    show(emptyOverview('30d', NOW), true);

    expect(target.textContent).not.toContain(translate('dashboard.empty'));
    expect(target.querySelectorAll('dd .animate-pulse')).toHaveLength(5);
  });
});
