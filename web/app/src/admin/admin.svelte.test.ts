import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import Admin from './Admin.svelte';
import type { Live, Overview, Range } from './api/types.ts';
import { adminTranslator } from './messages/catalog.ts';
import type { AdminLanguage } from './messages/catalog.ts';
import type { Notice, Stage } from './session/admin-area.svelte.ts';
import type { AdminClient, AdminSession } from './session/client.ts';
import { createAdminClient } from './session/client.ts';
import type { Problem } from './session/problems.ts';

interface Environment {
  readonly client: AdminClient;
  readonly now: () => number;
  readonly hidden: () => boolean;
}

const areas = vi.hoisted(() => {
  class FakeArea {
    stage: Stage = $state('signIn');
    busy = $state(false);
    problem: Problem | undefined = $state();
    notice: Notice | undefined = $state();
    range: Range = $state('7d');
    overview: Overview | undefined = $state.raw();
    live: Live | undefined = $state.raw();
    failed = $state(false);
    stale = $state(false);
    expiresAt: number | undefined = $state();
    readonly environment: Environment;
    readonly start = vi.fn<() => Promise<void>>(() => Promise.resolve());
    readonly enroll = vi.fn<() => Promise<void>>(() => Promise.resolve());
    readonly signIn = vi.fn<() => Promise<void>>(() => Promise.resolve());
    readonly signOut = vi.fn<() => Promise<void>>(() => Promise.resolve());
    readonly chooseRange = vi.fn<(range: Range) => void>();
    readonly wake = vi.fn<() => void>();

    constructor(environment: Environment) {
      this.environment = environment;
      areas.created.push(this);
    }
  }
  return { FakeArea, created: new Array<FakeArea>() };
});

vi.mock('./session/admin-area.svelte.ts', () => ({ AdminArea: areas.FakeArea }));
vi.mock(import('./session/client.ts'), () => ({ createAdminClient: vi.fn<typeof createAdminClient>() }));

const EXPIRES_AT = 1_700_000_000;

describe('admin page', () => {
  const client: AdminClient = {
    enrolled: () => Promise.resolve(true),
    enroll: () => Promise.resolve(),
    signIn: async () => Promise.withResolvers<AdminSession>().promise,
  };
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;

  function show(language: AdminLanguage = 'en') {
    component = mount(Admin, { target, props: { language } });
    flushSync();
    const area = areas.created.at(-1);
    if (!area) {
      throw new Error('the page created no admin area');
    }
    return area;
  }

  function labels(): string[] {
    return [...target.querySelectorAll('button')].map((candidate) => candidate.textContent.trim());
  }

  function button(label: string): HTMLButtonElement {
    const found = [...target.querySelectorAll('button')].find((candidate) => candidate.textContent.trim() === label);
    if (!found) {
      throw new Error(`missing button ${label}`);
    }
    return found;
  }

  function expiry(): string | undefined {
    return target.querySelector('header p')?.textContent.trim();
  }

  beforeEach(() => {
    vi.clearAllMocks();
    areas.created.length = 0;
    vi.mocked(createAdminClient).mockReturnValue(client);
    target = document.createElement('div');
    document.body.append(target);
  });

  afterEach(async () => {
    await unmount(component);
    target.remove();
  });

  it('starts the area with the admin client and the page visibility', () => {
    const area = show();

    expect(area.start).toHaveBeenCalledOnce();
    expect(area.environment.client).toBe(client);
    expect(area.environment.now()).toBeCloseTo(Date.now(), -3);
    expect(area.environment.hidden()).toBe(document.hidden);
  });

  it('shows the access screen in the page language until signed in', () => {
    const { translate } = adminTranslator('cs');
    show('cs');

    expect(target.querySelector('h1')?.textContent.trim()).toBe(translate('access.signIn.title'));
    expect(expiry()).toBeUndefined();
    expect(labels()).not.toContain(translate('access.signOut'));
    expect(target.querySelector('a')?.getAttribute('href')).toBe('/');
  });

  it('shows the dashboard, the session expiry and a sign-out once signed in', () => {
    const { translate } = adminTranslator('en');
    const area = show();

    area.expiresAt = EXPIRES_AT;
    area.stage = 'signedIn';
    flushSync();

    const time = new Intl.DateTimeFormat('en', { timeStyle: 'short' }).format(EXPIRES_AT * 1000);
    expect(expiry()).toBe(translate('access.expires', { time }));
    expect(target.querySelector('h1')?.textContent.trim()).toBe(translate('dashboard.title'));

    button(translate('dashboard.range.30d')).click();
    expect(area.chooseRange).toHaveBeenCalledExactlyOnceWith('30d');

    button(translate('access.signOut')).click();
    expect(area.signOut).toHaveBeenCalledOnce();
  });

  it('leaves the expiry out while it is unknown', () => {
    const { translate } = adminTranslator('en');
    const area = show();

    area.stage = 'signedIn';
    flushSync();

    expect(expiry()).toBeUndefined();
    expect(button(translate('access.signOut'))).toBeDefined();
  });

  it('wakes the area when the page becomes visible again', () => {
    const area = show();

    document.dispatchEvent(new Event('visibilitychange'));

    expect(area.wake).toHaveBeenCalledOnce();
  });
});
