import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Overview } from '../../api/types.ts';
import { loadWorldMap } from '../../map/world-map.ts';
import { adminTranslator } from '../../messages/catalog.ts';
import { getDashboardTools } from '../context.ts';
import { dashboardFormat } from '../format.ts';
import { emptyOverview } from '../overview.ts';
import WhereFrom from './WhereFrom.svelte';

vi.mock(import('../context.ts'), (original) =>
  original().then((actual) => ({ ...actual, getDashboardTools: vi.fn<typeof actual.getDashboardTools>() })),
);

const NOW = Date.UTC(2026, 10, 1, 12) / 1000;

describe('where people come from', () => {
  const { translate } = adminTranslator('en');
  const format = dashboardFormat('en');
  let target: HTMLElement;
  let component: ReturnType<typeof mount> | undefined;

  async function show(overview: Overview, loading = false): Promise<void> {
    component = mount(WhereFrom, { target, props: { overview, loading } });
    await loadWorldMap();
    await Promise.resolve();
    flushSync();
  }

  function rows(): string[] {
    return [...target.querySelectorAll('[role="region"] li .truncate')].map((label) => label.textContent.trim());
  }

  function caption(): string {
    return (
      target
        .querySelector('[aria-live="polite"]')
        ?.textContent.replaceAll(/[\t\n ]+/gu, ' ')
        .trim() ?? ''
    );
  }

  function counted(): string {
    return target.querySelector('h3 + p span')?.textContent.trim() ?? '';
  }

  const visited: Overview = {
    ...emptyOverview('30d', NOW),
    countries: [
      { name: 'unknown', value: 50 },
      { name: 'CZ', value: 10 },
      { name: 'DE', value: 30 },
      { name: 'FR', value: 0 },
    ],
    source: { countries: 'DB-IP Lite', attribution: 'IP data by DB-IP', attributionUrl: 'https://db-ip.com' },
  };

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

  it('lists the countries with visitors, largest first and the unknown country last', async () => {
    await show(visited);

    expect(rows()).toEqual([format.country('DE'), format.country('CZ'), translate('dashboard.where.unknown')]);
    expect(counted()).toBe(format.count(2));
    expect(caption()).toBe(translate('dashboard.where.hint'));
    const source = target.querySelector('a');
    expect(source?.getAttribute('href')).toBe('https://db-ip.com');
    expect(source?.textContent.trim()).toBe('IP data by DB-IP');
    expect(target.textContent).not.toContain(translate('dashboard.where.days'));
  });

  it('names the country picked in the list or on the map with its share of all visitors', async () => {
    await show(visited);

    target.querySelector<HTMLButtonElement>('[role="region"] li button')?.click();
    flushSync();
    expect(caption()).toBe(`${format.country('DE')} ${format.count(30)} · ${format.percent(30 / 90)}`);
    expect(target.querySelectorAll('g.pointer-events-none path')).toHaveLength(2);

    target
      .querySelector('path[data-country="CZ"]')
      ?.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' }));
    flushSync();
    expect(caption()).toBe(`${format.country('CZ')} ${format.count(10)} · ${format.percent(10 / 90)}`);
  });

  it('holds the list while the figures load', async () => {
    await show(visited, true);

    expect(rows()).toEqual([]);
    expect(counted()).toBe('–');
    expect(target.querySelector('a')).toBeNull();
  });

  it('says when nobody came yet and that hours are counted by whole days', async () => {
    await show({ ...emptyOverview('24h', NOW), countries: [{ name: 'DE', value: 0 }] });

    expect(rows()).toEqual([]);
    expect(caption()).toBe('');
    expect(target.textContent).toContain(translate('dashboard.where.empty'));
    expect(target.textContent).toContain(translate('dashboard.where.days'));
    expect(target.querySelector('a')).toBeNull();
  });
});
