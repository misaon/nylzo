// @vitest-environment happy-dom
import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ReportOutcome } from './report.ts';

import { getTranslator, Translator } from '../language/translator.svelte.ts';
import ReportForm from './ReportForm.svelte';

vi.mock(import('../language/translator.svelte.ts'), (original) =>
  // eslint-disable-next-line unicorn/prefer-await -- Svelte instruments awaits before hoisted imports initialize.
  original().then((actual) => ({ ...actual, getTranslator: vi.fn() })),
);

describe('report form', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;
  const translator = new Translator('en');
  const send = vi.fn<(submission: unknown) => Promise<ReportOutcome>>();
  const sent = vi.fn();

  function show(compact = false): void {
    component = mount(ReportForm, {
      target,
      props: { name: 'test-report', withLink: true, compact, onsubmit: send, onsent: sent },
    });
    flushSync();
  }

  function input(selector: string, value: string): void {
    const element = target.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector);
    if (!element) {
      throw new Error(`missing field ${selector}`);
    }
    element.value = value;
    element.dispatchEvent(new Event('input', { bubbles: true }));
  }

  function submit(): void {
    const form = target.querySelector('form');
    if (!form) {
      throw new Error('missing report form');
    }
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    flushSync();
  }

  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(getTranslator).mockReturnValue(translator);
    send.mockResolvedValue({ outcome: 'accepted' });
    target = document.createElement('div');
    document.body.append(target);
  });

  afterEach(async () => {
    await unmount(component);
    target.remove();
  });

  it('sends exactly the entered fields, blocks duplicate submissions, and clears accepted reports', async () => {
    const gate = Promise.withResolvers<ReportOutcome>();
    send.mockReturnValue(gate.promise);
    show();
    input('input[type="text"]', 'https://nylzo.test/m/synthetic#secret');
    input('textarea', 'synthetic reason');
    input('input[type="email"]', 'reporter@example.test');
    const checkbox = target.querySelector<HTMLInputElement>('input[type="checkbox"]');
    checkbox?.click();
    submit();
    submit();
    expect(send).toHaveBeenCalledExactlyOnceWith({
      link: 'https://nylzo.test/m/synthetic#secret',
      reason: 'synthetic reason',
      contact: 'reporter@example.test',
      goodFaith: true,
    });
    expect(target.querySelector('button')?.disabled).toBe(true);
    gate.resolve({ outcome: 'accepted' });
    await vi.waitFor(() => expect(sent).toHaveBeenCalledExactlyOnceWith('accepted'));
    expect(target.querySelector<HTMLInputElement>('input[type="text"]')?.value).toBe('');
    expect(target.querySelector('textarea')?.value).toBe('');
    expect(checkbox?.checked).toBe(false);
    expect(target.textContent).toContain(translator.translate('report.accepted'));
  });

  it.each([false, true])('shows errors and focuses the first invalid field, compact=%s', async (compact) => {
    send.mockResolvedValue({
      outcome: 'invalid',
      problems: ['link_invalid', 'reason_too_long', 'contact_invalid', 'good_faith_missing'],
    });
    show(compact);
    input('textarea', 'keep this draft');
    submit();
    await vi.waitFor(() => expect(target.querySelectorAll('[aria-invalid="true"]').length).toBe(compact ? 3 : 4));
    expect(document.activeElement).toBe(target.querySelector('[aria-invalid="true"]'));
    expect(target.textContent).toContain(translator.translate('report.problem.contact_invalid'));
    expect(target.textContent).toContain(translator.translate('report.problem.good_faith_missing'));
    expect(target.querySelector('textarea')?.value).toBe('keep this draft');
    expect(sent).not.toHaveBeenCalled();
  });

  it.each([false, true])('reports limits without clearing the draft, compact=%s', async (compact) => {
    send.mockResolvedValue({ outcome: 'limit' });
    show(compact);
    input('textarea', 'keep this report');
    submit();
    await vi.waitFor(() => expect(sent).toHaveBeenCalledExactlyOnceWith('limit'));
    expect(target.querySelector('textarea')?.value).toBe('keep this report');
    expect(target.textContent.includes(translator.translate('report.limit'))).toBe(!compact);
  });

  it.each<ReportOutcome>([{ outcome: 'failed' }, { outcome: 'ignored' }, { outcome: 'retry_later', retryAfter: 30 }])(
    'allows retry after $outcome without losing the entered reason',
    async (outcome) => {
      send.mockResolvedValueOnce(outcome);
      show();
      input('textarea', 'draft');
      submit();
      await vi.waitFor(() => expect(target.querySelector('button')?.disabled).toBe(false));
      expect(target.querySelector('textarea')?.value).toBe('draft');
      expect(sent).not.toHaveBeenCalled();
      submit();
      await vi.waitFor(() => expect(sent).toHaveBeenCalledExactlyOnceWith('accepted'));
    },
  );

  it('uses the supplied compact hint and keeps the optional link field out of the form', async () => {
    component = mount(ReportForm, {
      target,
      props: { name: 'compact-report', compact: true, hint: 'A synthetic hint', onsubmit: send },
    });
    flushSync();
    expect(target.querySelector('input[type="text"]')).toBeNull();
    expect(target.textContent).toContain('A synthetic hint');
    submit();
    await tick();
    expect(send).toHaveBeenCalledExactlyOnceWith({ link: '', reason: '', contact: '', goodFaith: false });
  });
});
