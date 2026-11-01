// @vitest-environment happy-dom
import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { CancelOutcome, ItemState, SentItem, SentView } from './sent-page.ts';

import { getTranslator, Translator } from '../../shared/language/translator.svelte.ts';
import { createSentPage } from './sent-page.ts';
import Sent from './Sent.svelte';

vi.mock('./sent-page.ts', () => ({ createSentPage: vi.fn() }));
vi.mock(import('../../shared/language/translator.svelte.ts'), (original) =>
  // eslint-disable-next-line unicorn/prefer-await -- Svelte instruments awaits before hoisted imports initialize.
  original().then((actual) => ({ ...actual, getTranslator: vi.fn() })),
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
  };

  function button(label: string): HTMLButtonElement {
    const element = [...target.querySelectorAll('button')].find((candidate) => candidate.textContent.trim() === label);
    if (!element) {
      throw new Error(`missing button ${label}`);
    }
    return element;
  }
  function item(state: ItemState = initialState): SentItem {
    return {
      id: 'AAAAAAAAAAAAAAAAAAAAAA',
      createdAt: NOW,
      expiresAt: NOW + 3_600_000,
      state,
      ended: false,
      cancellable: true,
    };
  }
  function show(items?: readonly SentItem[]): void {
    component = mount(Sent, { target });
    flushSync();
    if (!items) {
      return;
    }

    notify({ view: 'list', items });
    flushSync();
  }

  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(getTranslator).mockReturnValue(translator);
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
    expect(target.querySelector('[role="status"]')?.textContent).toBe(translator.translate('sent.loading'));
    notify({ view: 'unavailable' });
    flushSync();
    expect(target.querySelector('[role="alert"]')?.textContent).toBe(translator.translate('sent.unavailable'));
    notify({ view: 'list', items: [] });
    flushSync();
    expect(target.textContent).toContain(translator.translate('sent.empty'));
    expect(target.querySelector('button')).toBeNull();
  });

  it.each<ItemState>([
    { kind: 'loading' },
    { kind: 'unknown' },
    { kind: 'failed' },
    { kind: 'retry_later', retryAfter: 30 },
  ])('renders a $kind item and offers retry only for failures', async (state) => {
    show([item(state)]);
    expect(target.querySelector('h3')).not.toBeNull();
    const retry = [...target.querySelectorAll('button')].find(
      (candidate) => candidate.textContent === translator.translate('action.retry'),
    );
    expect(retry !== undefined).toBe(state.kind === 'failed' || state.kind === 'retry_later');
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
    });
    flushSync();
    expect(target.textContent).toContain(translator.translate('sent.not_kept'));
    expect(button(translator.translate('sent.cancel')).disabled).toBe(true);
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
    button(translator.translate('sent.forget')).click();
    flushSync();
    expect(page.forget).not.toHaveBeenCalled();
    button(translator.translate('sent.forget.yes')).click();
    await vi.waitFor(() => expect(page.forget).toHaveBeenCalledExactlyOnceWith(item().id));
  });

  it('forgets an ended record directly, and confirms before forgetting the whole list', async () => {
    show([{ ...item(), cancellable: false, ended: true }]);
    button(translator.translate('sent.forget')).click();
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
    button(translator.translate('sent.forget')).click();
    await vi.waitFor(() =>
      expect(target.querySelector('[role="status"]')?.textContent).toBe(translator.translate('sent.unavailable')),
    );
    expect(document.activeElement).toBe(target.querySelector('h2'));
  });
});
