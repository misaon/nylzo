import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { formatMoment } from '../../shared/language/format.ts';
import { getTranslator, Translator } from '../../shared/language/translator.svelte.ts';
import { servedByLocalClient } from '../../shared/local-client/instance-origin.ts';
import { getSessionSentStore, sessionSentStore } from '../../shared/sent-records/session-sent-store.ts';
import type { CancelOutcome, ItemState, SentItem, SentView } from './sent-page.ts';
import { createSentPage } from './sent-page.ts';
import Sent from './Sent.svelte';

vi.mock('./sent-page.ts', () => ({ createSentPage: vi.fn<typeof import('./sent-page.ts').createSentPage>() }));
vi.mock(import('../../shared/language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);
vi.mock(import('../../shared/local-client/instance-origin.ts'), (original) =>
  original().then((actual) => ({ ...actual, servedByLocalClient: vi.fn<typeof actual.servedByLocalClient>() })),
);
vi.mock(import('../../shared/sent-records/session-sent-store.ts'), (original) =>
  original().then((actual) => ({ ...actual, getSessionSentStore: vi.fn<typeof actual.getSessionSentStore>() })),
);

const NOW = Date.parse('2026-11-01T12:00:00Z');

const initialState: ItemState = {
  kind: 'loaded',
  status: { state: 'available', opensUsed: 1, opensMax: 3, failedPassphraseAttempts: 2 },
};

describe('sent messages interface', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;
  let notify: (view: SentView) => void;
  const translator = new Translator('en');
  const page = {
    load: vi.fn<() => Promise<void>>(),
    refresh: vi.fn<(id: string) => Promise<void>>(),
    cancel: vi.fn<(id: string) => Promise<CancelOutcome>>(),
    forgetAll: vi.fn<() => Promise<void>>(),
    stop: vi.fn<() => void>(),
  };

  function button(label: string): HTMLButtonElement {
    const element = [...target.querySelectorAll('button')].find((candidate) => candidate.textContent.trim() === label);
    if (!element) {
      throw new Error(`missing button ${label}`);
    }
    return element;
  }

  function show(items?: readonly SentItem[]): void {
    component = mount(Sent, { target });
    flushSync();
    if (!items) {
      return;
    }

    notify({ view: 'list', items, storageFailed: false });
    flushSync();
  }

  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(getTranslator).mockReturnValue(translator);
    vi.mocked(getSessionSentStore).mockReturnValue(sessionSentStore());
    page.load.mockResolvedValue();
    page.refresh.mockResolvedValue();
    page.forgetAll.mockResolvedValue();
    page.cancel.mockResolvedValue({ outcome: 'cancelled' });
    vi.mocked(createSentPage).mockImplementation((_dependencies, onView) => {
      notify = onView;
      return page;
    });
    target = document.createElement('div');
    document.body.append(target);
  });
  afterEach(async () => {
    await unmount(component);
    target.remove();
    vi.restoreAllMocks();
  });

  function status(): string | undefined {
    return target.querySelector('[role="status"]')?.textContent.trim();
  }

  it.each([
    { served: false, browserStore: true },
    { served: true, browserStore: false },
  ])(
    'opens browser storage only outside a session of the local client ($served) and reads the system clock',
    ({ served, browserStore }) => {
      vi.mocked(servedByLocalClient).mockReturnValue(served);
      vi.spyOn(Date, 'now').mockReturnValue(NOW);
      show();
      const dependencies = vi.mocked(createSentPage).mock.calls[0]?.[0];
      expect(dependencies?.browser !== undefined).toBe(browserStore);
      expect(dependencies?.now()).toBe(NOW);
    },
  );

  it('announces loading, unavailable storage and an empty list', () => {
    show();
    expect(page.load).toHaveBeenCalledOnce();
    expect(target.textContent).toContain(translator.translate('sent.loading'));
    notify({ view: 'unavailable' });
    flushSync();
    expect(target.querySelector('[role="alert"]')?.textContent.trim()).toBe(translator.translate('sent.unavailable'));
    notify({ view: 'list', items: [], storageFailed: false });
    flushSync();
    expect(target.textContent).toContain(translator.translate('sent.empty'));
    expect(target.querySelectorAll('button')).toHaveLength(1);
    expect(target.querySelector('button')?.classList).toContain('invisible');
  });

  it.each<ItemState>([
    { kind: 'loading' },
    { kind: 'unknown' },
    { kind: 'failed' },
    { kind: 'retry_later', retryAfter: 30 },
  ])('renders a $kind item and offers retry when the status did not load', async (state) => {
    show([item(state)]);
    expect(target.querySelector('h2')).not.toBeNull();
    const retry = [...target.querySelectorAll('button')].find(
      (candidate) => candidate.textContent.trim() === translator.translate('action.retry'),
    );
    expect(retry !== undefined).toBe(state.kind !== 'loaded' && state.kind !== 'loading');
    if (!retry) {
      return;
    }

    retry.click();
    await tick();
    expect(page.refresh).toHaveBeenCalledExactlyOnceWith(item().id);
  });

  it('renders opens, attempts, first-opened time and an ended timestamp without retaining counters', async () => {
    const openedAt = new Date(NOW + 1000).toISOString();
    const loaded: ItemState = {
      kind: 'loaded',
      status: { state: 'opened', opensUsed: 1, opensMax: 3, failedPassphraseAttempts: 2, firstOpenedAt: openedAt },
    };
    show([item(loaded)]);
    expect(target.textContent).toContain(translator.translate('sent.opens', { used: 1, max: 3 }));
    notify({
      view: 'list',
      items: [
        {
          ...item(),
          ended: true,
          cancellable: false,
          state: { kind: 'loaded', status: { ...loaded.status, endedAt: openedAt, state: 'expired' } },
        },
      ],
      storageFailed: false,
    });
    flushSync();
    expect(target.textContent).toContain(translator.translate('sent.state.expired'));
    expect(target.textContent).toContain(
      translator.translate('sent.ended.expired', { time: formatMoment(openedAt, translator.language) }),
    );
    await expect
      .poll(() => target.textContent, { timeout: 5000 })
      .not.toContain(translator.translate('sent.opens', { used: 1, max: 3 }));
    expect(() => button(translator.translate('sent.cancel'))).toThrow('missing button');
  });

  it('stops loading and clears what it showed, an open dialog included, when the page is left or replaced', async () => {
    show([{ ...item(), label: 'Password for Peter' }]);
    button(translator.translate('sent.cancel')).click();
    flushSync();
    expect(target.querySelector('dialog')?.textContent).toContain('Password for Peter');
    dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }));
    expect(target.querySelector('dialog')).toBeNull();
    expect(page.stop).toHaveBeenCalledOnce();
    await unmount(component);
    expect(page.stop).toHaveBeenCalledTimes(2);
    show();
  });

  it('marks a message that only this tab tracks', () => {
    const time = formatMoment(NOW, translator.language);
    show([item(), { ...item(), id: 'AQEBAQEBAQEBAQEBAQEBAQ', session: true }]);
    const captions = [...target.querySelectorAll('li p > span:last-child')].map((caption) => caption.textContent);
    expect(captions).toEqual([
      translator.translate('sent.created', { time }),
      translator.translate('sent.created.session', { time }),
    ]);
  });

  it.each<CancelOutcome>([{ outcome: 'cancelled' }, { outcome: 'failed' }, { outcome: 'retry_later', retryAfter: 30 }])(
    'confirms cancellation and announces $outcome',
    async (outcome) => {
      page.cancel.mockResolvedValue(outcome);
      show([item()]);
      button(translator.translate('sent.cancel')).click();
      flushSync();
      expect(target.querySelector('dialog')?.open).toBe(true);
      button(translator.translate('sent.cancel.yes')).click();
      await vi.waitFor(() => expect(page.cancel).toHaveBeenCalledExactlyOnceWith(item().id));
      await tick();
      expect(target.querySelector('dialog')).toBeNull();
      expect(target.querySelector('[role="status"]')?.textContent).not.toBe('');
    },
  );

  it.each<CancelOutcome>([{ outcome: 'ended' }, { outcome: 'unknown' }, { outcome: 'ignored' }])(
    'confirms cancellation and announces nothing when the outcome is $outcome',
    async (outcome) => {
      page.cancel.mockResolvedValue(outcome);
      show([item()]);
      button(translator.translate('sent.cancel')).click();
      flushSync();
      button(translator.translate('sent.cancel.yes')).click();
      await vi.waitFor(() => expect(page.cancel).toHaveBeenCalledOnce());
      await tick();
      expect(status()).toBe('');
    },
  );

  it('cancels nothing when a confirmation lands after the page was left', async () => {
    const closing = Promise.withResolvers<undefined>();
    show([item()]);
    button(translator.translate('sent.cancel')).click();
    flushSync();
    const dialog = target.querySelector('dialog');
    if (!dialog) {
      throw new Error('missing dialog');
    }
    vi.spyOn(dialog, 'getAnimations').mockReturnValue([
      Object.assign(new Animation(), { finished: closing.promise.then(() => new Animation()) }),
    ]);
    button(translator.translate('sent.cancel.yes')).click();
    dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }));
    closing.resolve(undefined);
    await tick();
    await tick();
    expect(page.cancel).not.toHaveBeenCalled();
  });

  it('loads again only when the browser restores the page from its back-forward cache', () => {
    show();
    dispatchEvent(Object.assign(new Event('pageshow'), { persisted: false }));
    expect(page.load).toHaveBeenCalledOnce();
    dispatchEvent(Object.assign(new Event('pageshow'), { persisted: true }));
    expect(page.load).toHaveBeenCalledTimes(2);
  });

  it('keeps the place of the cancel button while a status loads and drops it for a message nobody can cancel', () => {
    show([{ ...item({ kind: 'loading' }), cancellable: false }]);
    expect(button(translator.translate('sent.cancel')).classList).toContain('invisible');
    notify({ view: 'list', items: [{ ...item({ kind: 'unknown' }), cancellable: false }], storageFailed: false });
    flushSync();
    expect(button(translator.translate('action.retry'))).toBeDefined();
    expect(() => button(translator.translate('sent.cancel'))).toThrow('missing button');
  });

  it('marks a fully opened message with a check and a live message without failed attempts with no warning', () => {
    const opened: ItemState = {
      kind: 'loaded',
      status: {
        state: 'opened',
        opensUsed: 1,
        opensMax: 1,
        failedPassphraseAttempts: 0,
        endedAt: new Date(NOW).toISOString(),
      },
    };
    const live: ItemState = {
      kind: 'loaded',
      status: { state: 'available', opensUsed: 0, opensMax: 1, failedPassphraseAttempts: 0 },
    };
    show([
      { ...item(opened), ended: true, cancellable: false },
      { ...item(live), id: 'AQEBAQEBAQEBAQEBAQEBAQ' },
    ]);
    const [endedBadge, liveBadge] = [...target.querySelectorAll('h2 + span')];
    expect(endedBadge?.querySelector('svg')).not.toBeNull();
    expect(liveBadge?.querySelector('svg')).toBeNull();
    expect(target.querySelectorAll('li')[1]?.querySelector('.text-rose-200')).toBeNull();
  });

  it('glides the remaining messages only when the list itself changes', async () => {
    let measured = 0;
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
      () => new DOMRect(0, (measured += 1), 100, 40),
    );
    const animate = vi.spyOn(Element.prototype, 'animate');
    const second = { ...item(), id: 'AQEBAQEBAQEBAQEBAQEBAQ' };
    show([item(), second]);
    const remaining = target.querySelector(`#sent-item-${second.id}`)?.closest('li');
    const glided = () => animate.mock.contexts.includes(remaining);
    notify({ view: 'list', items: [{ ...item(), state: { kind: 'loading' } }, second], storageFailed: false });
    flushSync();
    await vi.waitFor(() => expect(measured).toBe(4));
    await tick();
    expect(glided()).toBe(false);
    notify({ view: 'list', items: [second], storageFailed: false });
    flushSync();
    await vi.waitFor(() => expect(glided()).toBe(true));
  });

  it('dismisses confirmation without cancelling', async () => {
    show([item()]);
    button(translator.translate('sent.cancel')).click();
    flushSync();
    button(translator.translate('sent.cancel.no')).click();
    await tick();
    expect(page.cancel).not.toHaveBeenCalled();
    expect(target.querySelector('dialog')).toBeNull();
  });

  it('returns focus to the page when forgetting the list is dismissed', async () => {
    show([item()]);
    button(translator.translate('sent.forget_all')).click();
    flushSync();
    button(translator.translate('sent.keep')).click();
    await vi.waitFor(() => expect(document.activeElement).toBe(target.querySelector('h1')));
    expect(page.forgetAll).not.toHaveBeenCalled();
  });

  it('confirms before forgetting the whole list', async () => {
    show([{ ...item(), cancellable: false, ended: true }]);
    button(translator.translate('sent.forget_all')).click();
    flushSync();
    expect(page.forgetAll).not.toHaveBeenCalled();
    button(translator.translate('sent.forget_all.yes')).click();
    await vi.waitFor(() => expect(page.forgetAll).toHaveBeenCalledOnce());
  });

  it('shows storage failure if forgetting fails and returns focus to the page', async () => {
    page.forgetAll.mockRejectedValue(new Error('storage denied'));
    show([{ ...item(), cancellable: false }]);
    button(translator.translate('sent.forget_all')).click();
    flushSync();
    button(translator.translate('sent.forget_all.yes')).click();
    await vi.waitFor(() =>
      expect(target.querySelector('[role="status"]')?.textContent).toBe(translator.translate('sent.unavailable')),
    );
    expect(document.activeElement).toBe(target.querySelector('h1'));
  });
});

function item(state: ItemState = initialState): SentItem {
  return {
    id: 'AAAAAAAAAAAAAAAAAAAAAA',
    createdAt: NOW,
    expiresAt: NOW + 3_600_000,
    state,
    ended: false,
    cancellable: true,
    session: false,
  };
}
