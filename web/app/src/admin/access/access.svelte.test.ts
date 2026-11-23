import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../../shared/api/api-errors.ts';
import type { Live, Overview } from '../api/types.ts';
import { adminTranslator } from '../messages/catalog.ts';
import type { AdminMessageKey } from '../messages/catalog.ts';
import { getAdminTranslator } from '../messages/translator-context.ts';
import { AdminArea } from '../session/admin-area.svelte.ts';
import type { AdminClient, AdminSession } from '../session/client.ts';
import { passkeysSupported } from '../session/passkey.ts';
import Access from './Access.svelte';

vi.mock(import('../messages/translator-context.ts'), (original) =>
  original().then((actual) => ({ ...actual, getAdminTranslator: vi.fn<typeof actual.getAdminTranslator>() })),
);
vi.mock(import('../session/passkey.ts'), () => ({
  passkeysSupported: vi.fn<typeof import('../session/passkey.ts').passkeysSupported>(),
  createPasskey: vi.fn<typeof import('../session/passkey.ts').createPasskey>(),
  usePasskey: vi.fn<typeof import('../session/passkey.ts').usePasskey>(),
}));

const TOKEN = `${'B'.repeat(31)}w`;

describe('access screens', () => {
  const { translate } = adminTranslator('en');
  const session: AdminSession = {
    expiresAt: 100,
    overview: async () => Promise.withResolvers<Overview>().promise,
    live: async () => Promise.withResolvers<Live>().promise,
    signOut: () => Promise.resolve(),
  };
  const client = {
    enrolled: vi.fn<AdminClient['enrolled']>(),
    enroll: vi.fn<AdminClient['enroll']>(),
    signIn: vi.fn<AdminClient['signIn']>(),
  };
  let target: HTMLElement;
  let component: ReturnType<typeof mount> | undefined;
  let area: AdminArea;

  function show(): void {
    component = mount(Access, { target, props: { area } });
    flushSync();
  }

  async function started(): Promise<void> {
    await area.start();
    flushSync();
  }

  function heading(): string | undefined {
    return target.querySelector('h1')?.textContent.trim();
  }

  function buttons(): HTMLButtonElement[] {
    return [...target.querySelectorAll('button')];
  }

  function button(key: AdminMessageKey): HTMLButtonElement {
    const found = buttons().find((candidate) => candidate.textContent.trim() === translate(key));
    if (!found) {
      throw new Error(`missing button ${key}`);
    }
    return found;
  }

  function statuses(): string[] {
    return [...target.querySelectorAll('[role="status"]')].map((element) => element.textContent.trim());
  }

  function alert(): string | undefined {
    return target.querySelector('[role="alert"]')?.textContent.trim();
  }

  beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal('isSecureContext', true);
    vi.mocked(getAdminTranslator).mockReturnValue(adminTranslator('en'));
    vi.mocked(passkeysSupported).mockResolvedValue(true);
    client.enrolled.mockResolvedValue(true);
    client.signIn.mockResolvedValue(session);
    history.replaceState(null, '', '/admin');
    area = new AdminArea({ client, now: () => 0, hidden: () => true });
    target = document.createElement('div');
    document.body.append(target);
  });

  afterEach(async () => {
    vi.unstubAllGlobals();
    if (component) {
      await unmount(component);
    }
    component = undefined;
    target.remove();
  });

  it('shows only a loading card until the area knows where it stands', () => {
    show();

    expect(statuses()).toEqual([translate('access.loading')]);
    expect(heading()).toBe(translate('access.area'));
    expect(buttons()).toEqual([]);
  });

  it('lists the enrollment commands for an instance without a passkey', async () => {
    client.enrolled.mockResolvedValue(false);
    show();
    await started();

    expect(heading()).toBe(translate('access.unconfigured.title'));
    expect([...target.querySelectorAll('code')].map((code) => code.textContent.replace(/^\$\s*/u, ''))).toEqual([
      'nylzo admin-enroll',
      'docker compose exec -T nylzo /ko-app/nylzo admin-enroll',
    ]);
    expect(buttons().every((candidate) => candidate.textContent.trim() === translate('access.copy'))).toBe(true);
  });

  it('enrolls from an enrollment link and shows the ceremony as busy while it runs', async () => {
    history.replaceState(null, '', `/admin#enroll=${TOKEN}`);
    const enrollment = Promise.withResolvers<undefined>();
    client.enroll.mockReturnValue(enrollment.promise);
    client.signIn.mockRejectedValue(new DOMException('click again', 'NotAllowedError'));
    show();
    await started();
    expect(heading()).toBe(translate('access.enroll.title'));

    const action = button('access.enroll.action');
    action.click();
    flushSync();
    expect(client.enroll).toHaveBeenCalledWith(TOKEN);
    expect(action.disabled).toBe(true);
    expect(action.getAttribute('aria-busy')).toBe('true');
    expect(statuses()).toContain(translate('access.waiting'));

    enrollment.resolve(undefined);
    await vi.waitFor(() => {
      flushSync();
      expect(heading()).toBe(translate('access.signIn.title'));
    });
    expect(statuses()).toContain(translate('access.notice.enrolled'));
    expect(alert()).toBeUndefined();
    expect(button('access.signIn.action').disabled).toBe(false);
  });

  it('signs in with the passkey and shows why a sign-in failed', async () => {
    client.signIn.mockRejectedValue(new ApiError(401, 'unexpected'));
    show();
    await started();
    expect(heading()).toBe(translate('access.signIn.title'));

    button('access.signIn.action').click();
    await vi.waitFor(() => {
      flushSync();
      expect(alert()).toBe(translate('access.problem.refused'));
    });
    expect(client.signIn).toHaveBeenCalledOnce();
  });

  it('confirms a sign-out on the sign-in screen only and moves focus to it', async () => {
    show();
    await started();
    await area.signOut();
    flushSync();
    expect(statuses()).toContain(translate('access.notice.signedOut'));
    expect(document.activeElement).toBe(target.querySelector('h1'));

    client.enrolled.mockRejectedValue(new ApiError(500, 'internal'));
    await started();
    expect(statuses()).not.toContain(translate('access.notice.signedOut'));
  });

  it('offers a retry when the server could not be asked', async () => {
    client.enrolled.mockRejectedValueOnce(new ApiError(503, 'unavailable'));
    show();
    await started();
    expect(heading()).toBe(translate('access.unavailable.title'));
    expect(alert()).toBe(translate('access.problem.busy'));

    button('access.retry').click();
    await vi.waitFor(() => {
      flushSync();
      expect(heading()).toBe(translate('access.signIn.title'));
    });
    expect(alert()).toBeUndefined();
  });

  it('explains a browser without passkeys and offers no action', async () => {
    vi.mocked(passkeysSupported).mockResolvedValue(false);
    show();
    await started();

    expect(heading()).toBe(translate('access.unsupported.title'));
    expect(buttons()).toEqual([]);
  });

  it('shows no access screen once signed in', async () => {
    show();
    await started();
    await area.signIn();
    flushSync();

    expect(area.stage).toBe('signedIn');
    expect(buttons()).toEqual([]);
    expect(statuses()).toEqual([translate('access.loading')]);
  });
});
