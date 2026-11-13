// @vitest-environment happy-dom
import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { formatMoment } from '../../shared/language/format.ts';
import { getTranslator, Translator } from '../../shared/language/translator.svelte.ts';
import { getSessionSentStore, sessionSentStore } from '../../shared/sent-records/session-sent-store.ts';
import type { CancelOutcome, ItemState, SentItem, SentView } from './sent-page.ts';
import { createSentPage } from './sent-page.ts';
import Sent from './Sent.svelte';

vi.mock('./sent-page.ts', () => ({ createSentPage: vi.fn<typeof import('./sent-page.ts').createSentPage>() }));
vi.mock(import('../../shared/language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
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
    forget: vi.fn<(id: string) => Promise<void>>(),
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

  function forgetButton(): HTMLButtonElement {
    const label = translator.translate('sent.forget.label');
    const element = [...target.querySelectorAll('button')].find((candidate) => candidate.textContent.includes(label));
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
    page.forget.mockResolvedValue();
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
  });

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

  it('renders opens, attempts, first-opened time and an ended timestamp without retaining counters', () => {
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
    expect(target.textContent).not.toContain(translator.translate('sent.opens', { used: 1, max: 3 }));
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
    const captions = [...target.querySelectorAll('li p.text-caption')].map((caption) => caption.textContent);
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

  it('dismisses confirmation without cancelling and asks before forgetting a live message', async () => {
    show([item()]);
    button(translator.translate('sent.cancel')).click();
    flushSync();
    button(translator.translate('sent.cancel.no')).click();
    await tick();
    expect(page.cancel).not.toHaveBeenCalled();
    forgetButton().click();
    flushSync();
    expect(page.forget).not.toHaveBeenCalled();
    button(translator.translate('sent.forget.yes')).click();
    await vi.waitFor(() => expect(page.forget).toHaveBeenCalledExactlyOnceWith(item().id));
  });

  it('forgets an ended record directly, and confirms before forgetting the whole list', async () => {
    show([{ ...item(), cancellable: false, ended: true }]);
    forgetButton().click();
    await tick();
    expect(page.forget).toHaveBeenCalledExactlyOnceWith(item().id);
    expect(target.querySelector('dialog')).toBeNull();
    button(translator.translate('sent.forget_all')).click();
    flushSync();
    expect(page.forgetAll).not.toHaveBeenCalled();
    button(translator.translate('sent.forget_all.yes')).click();
    await vi.waitFor(() => expect(page.forgetAll).toHaveBeenCalledOnce());
  });

  it('shows storage failure if forgetting fails and returns focus to the page', async () => {
    page.forget.mockRejectedValue(new Error('storage denied'));
    show([{ ...item(), cancellable: false }]);
    forgetButton().click();
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
