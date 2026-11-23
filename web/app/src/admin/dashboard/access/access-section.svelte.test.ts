import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Overview } from '../../api/types.ts';
import { adminTranslator } from '../../messages/catalog.ts';
import { getDashboardTools } from '../context.ts';
import { dashboardFormat } from '../format.ts';
import { emptyOverview } from '../overview.ts';
import AccessSection from './AccessSection.svelte';

vi.mock(import('../context.ts'), (original) =>
  original().then((actual) => ({ ...actual, getDashboardTools: vi.fn<typeof actual.getDashboardTools>() })),
);

const NOW = Date.UTC(2026, 10, 1, 12) / 1000;

interface SectionProps {
  readonly security: Overview['security'];
  readonly sessionExpiresAt: number;
  readonly loading: boolean;
}

describe('access section', () => {
  const { translate } = adminTranslator('en');
  const format = dashboardFormat('en');
  let target: HTMLElement;
  let component: ReturnType<typeof mount> | undefined;

  function show({ security, sessionExpiresAt, loading }: SectionProps): void {
    const overview = { ...emptyOverview('30d', NOW), security };
    component = mount(AccessSection, { target, props: { overview, sessionExpiresAt, loading, now: NOW } });
    flushSync();
  }

  function figure(label: string): Element | null | undefined {
    return [...target.querySelectorAll('dt')].find((element) => element.textContent.trim() === label)
      ?.nextElementSibling;
  }

  function time(label: string): string | null | undefined {
    return figure(label)?.querySelector('time')?.getAttribute('datetime');
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

  it('shows the passkeys, the previous sign-in and when the live session ends', () => {
    show({
      security: { passkeys: 2, lastSignIn: NOW - 7200, sessionExpiresAt: NOW + 600 },
      sessionExpiresAt: NOW + 1800,
      loading: false,
    });

    expect(figure(translate('dashboard.access.passkeys'))?.textContent.trim()).toBe(format.count(2));
    expect(figure(translate('dashboard.access.last'))?.textContent.trim()).toBe(format.relative(NOW - 7200, NOW));
    expect(time(translate('dashboard.access.last'))).toBe(new Date((NOW - 7200) * 1000).toISOString());
    expect(time(translate('dashboard.access.session'))).toBe(new Date((NOW + 1800) * 1000).toISOString());
  });

  it('falls back to the session end of the overview and never puts the previous sign-in in the future', () => {
    show({
      security: { passkeys: 1, lastSignIn: NOW + 30, sessionExpiresAt: NOW + 600 },
      sessionExpiresAt: 0,
      loading: false,
    });

    expect(figure(translate('dashboard.access.last'))?.textContent.trim()).toBe(format.relative(NOW, NOW));
    expect(time(translate('dashboard.access.session'))).toBe(new Date((NOW + 600) * 1000).toISOString());
  });

  it('says when nobody signed in before and when no session end is known', () => {
    show({ security: { passkeys: 1, lastSignIn: 0, sessionExpiresAt: 0 }, sessionExpiresAt: 0, loading: false });

    expect(figure(translate('dashboard.access.last'))?.textContent.trim()).toBe(translate('dashboard.access.never'));
    expect(figure(translate('dashboard.access.session'))?.textContent.trim()).toMatch(/^–/u);
  });

  it('holds the figures while they load', () => {
    show({ security: { passkeys: 3, lastSignIn: NOW, sessionExpiresAt: 0 }, sessionExpiresAt: 0, loading: true });

    expect(figure(translate('dashboard.access.passkeys'))?.textContent.trim()).toBe('–');
    expect(figure(translate('dashboard.access.last'))?.textContent.trim()).toBe('–');
  });
});
