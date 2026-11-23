import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Overview, TimelineMetric } from '../../api/types.ts';
import { byTimelineMetric } from '../../api/types.ts';
import { adminTranslator } from '../../messages/catalog.ts';
import { getDashboardTools } from '../context.ts';
import { dashboardFormat } from '../format.ts';
import { emptyOverview } from '../overview.ts';
import Breakdowns from './Breakdowns.svelte';
import TrafficSection from './TrafficSection.svelte';

vi.mock(import('../context.ts'), (original) =>
  original().then((actual) => ({ ...actual, getDashboardTools: vi.fn<typeof actual.getDashboardTools>() })),
);

const NOW = Date.UTC(2026, 10, 1, 12) / 1000;

const TOTALS: Readonly<Partial<Record<TimelineMetric, number>>> = {
  'views.people': 120,
  visitors: 40,
  'views.robots': 60,
  'views.unconfirmed': 20,
};
const PREVIOUS: Readonly<Partial<Record<TimelineMetric, number>>> = {
  'views.people': 100,
  visitors: 50,
  'views.robots': 60,
  'views.unconfirmed': 0,
};

function visited(range: '24h' | '30d'): Overview {
  return {
    ...emptyOverview(range, NOW),
    totals: byTimelineMetric((metric) => TOTALS[metric] ?? 0),
    previous: byTimelineMetric((metric) => PREVIOUS[metric] ?? 0),
  };
}

function text(element: Element | null | undefined): string {
  return element?.textContent.replaceAll(/[\t\n ]+/gu, ' ').trim() ?? '';
}

describe('traffic', () => {
  const { translate } = adminTranslator('en');
  const format = dashboardFormat('en');
  let target: HTMLElement;
  let component: ReturnType<typeof mount> | undefined;

  function stat(label: string): string {
    const term = [...target.querySelectorAll('dt')].find((element) => text(element) === label);
    return text(term?.nextElementSibling);
  }

  function clear(): void {
    if (component) {
      void unmount(component);
      component = undefined;
    }
    target.replaceChildren();
  }

  function legend(): string[] {
    return [...target.querySelectorAll('h3 + ul li')].map((item) => text(item));
  }

  beforeEach(() => {
    vi.mocked(getDashboardTools).mockReturnValue({ translate, format });
    target = document.createElement('div');
    document.body.append(target);
  });
  afterEach(() => {
    clear();
    target.remove();
  });

  function showSection(overview: Overview, loading = false): void {
    component = mount(TrafficSection, { target, props: { overview, range: overview.range, loading } });
    flushSync();
  }

  function showBreakdowns(overview: Overview, loading = false): void {
    component = mount(Breakdowns, { target, props: { overview, loading } });
    flushSync();
  }

  function labels(list: string): string[] {
    return [...target.querySelectorAll(`ul[aria-label="${list}"] .truncate`)].map((label) => text(label));
  }

  describe('section', () => {
    it('compares daily views and visitors with the period before and draws both', () => {
      showSection(visited('30d'));

      expect(stat(translate('dashboard.traffic.people'))).toContain(`${format.count(120)} ${format.change(0.2)}`);
      expect(stat(translate('dashboard.traffic.visitors'))).toContain(format.change(-0.2));
      expect(stat(translate('dashboard.traffic.visitors'))).toContain(translate('dashboard.traffic.visitors.note'));
      expect(stat(translate('dashboard.traffic.robots'))).toContain(format.change(0));
      expect(stat(translate('dashboard.traffic.unconfirmed'))).not.toContain('%');
      expect(legend()).toEqual([translate('dashboard.traffic.people'), translate('dashboard.traffic.visitors')]);
      expect(target.textContent).toContain(format.percent(120 / 200));
      expect(target.textContent).not.toContain(translate('dashboard.empty'));
    });

    it('leaves visitors out of the hours and does not compare them', () => {
      showSection(visited('24h'));

      const visitors = stat(translate('dashboard.traffic.visitors'));
      expect(visitors).toContain(translate('dashboard.traffic.visitors.days'));
      expect(visitors).not.toContain('%');
      expect(legend()).toEqual([translate('dashboard.traffic.people')]);
    });

    it('says when nobody came and holds the shares while loading', () => {
      showSection(emptyOverview('30d', NOW));
      expect(target.textContent).toContain(translate('dashboard.empty'));
      clear();
      showSection(emptyOverview('30d', NOW), true);
      expect(target.textContent).not.toContain(translate('dashboard.empty'));
      const shares = target.querySelector(`ul[aria-label="${translate('dashboard.traffic.share')}"]`);
      expect(text(shares)).not.toMatch(/\d/u);
    });
  });

  describe('breakdowns', () => {
    it('lists the pages people viewed and the kinds of robots that came', () => {
      showBreakdowns({
        ...emptyOverview('30d', NOW),
        pages: [
          { name: 'home', value: 5 },
          { name: 'retired-page', value: 2 },
          { name: 'terms', value: 0 },
        ],
        robots: [
          { name: 'ai', value: 3 },
          { name: 'new-kind', value: 1 },
        ],
      });

      expect(labels(translate('dashboard.pages.list'))).toEqual([translate('dashboard.page.home'), 'retired-page']);
      expect(labels(translate('dashboard.traffic.robots.title'))).toEqual([
        translate('dashboard.robot.ai'),
        'new-kind',
      ]);
    });

    it('says when there is nothing to list and holds the lists while loading', () => {
      showBreakdowns(emptyOverview('30d', NOW));
      expect(target.textContent).toContain(translate('dashboard.pages.empty'));
      expect(target.textContent).toContain(translate('dashboard.traffic.robots.empty'));
      clear();
      showBreakdowns(emptyOverview('30d', NOW), true);
      expect(target.querySelectorAll('.animate-pulse')).toHaveLength(10);
      expect(target.textContent).not.toContain(translate('dashboard.pages.empty'));
    });
  });
});
