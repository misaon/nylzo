import { requestKey } from '@nylzo/core';
import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { createApi } from '../../../shared/api/api.ts';
import { fakeApi } from '../../../shared/api/fake-api.ts';
import type { FakeApi } from '../../../shared/api/fake-api.ts';
import type { MessageKey } from '../../../shared/language/catalog.ts';
import { formatBytes } from '../../../shared/language/format.ts';
import { getTranslator, Translator } from '../../../shared/language/translator.svelte.ts';
import { getScreenWork, ScreenWork } from '../../../shared/navigation/screen-work.ts';
import { browserSentStore, rememberSent } from '../../../shared/sent-records/sent-store.ts';
import { getSessionSentStore, sessionSentStore } from '../../../shared/sent-records/session-sent-store.ts';
import { createMessage } from '../create-message.ts';
import type { Created } from '../create-message.ts';
import ComposerSection from './ComposerSection.svelte';

vi.mock('../../../shared/api/api.ts', () => ({
  createApi: vi.fn<typeof import('../../../shared/api/api.ts').createApi>(),
}));
vi.mock('../../../shared/sent-records/sent-store.ts', () => ({
  browserSentStore: vi.fn<(...arguments_: unknown[]) => void>(),
  rememberSent: vi.fn<(...arguments_: unknown[]) => void>(),
}));
vi.mock(import('../create-message.ts'), (original) =>
  original().then((actual) => ({ ...actual, createMessage: vi.fn<typeof actual.createMessage>() })),
);
vi.mock(import('../../../shared/language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);
vi.mock(import('../../../shared/navigation/screen-work.ts'), (original) =>
  original().then((actual) => ({ ...actual, getScreenWork: vi.fn<typeof actual.getScreenWork>() })),
);
vi.mock(import('../../../shared/sent-records/session-sent-store.ts'), (original) =>
  original().then((actual) => ({ ...actual, getSessionSentStore: vi.fn<typeof actual.getSessionSentStore>() })),
);

class RecordingObserver {
  readonly observed = new Set<Element>();

  observe(element: Element): void {
    this.observed.add(element);
  }

  unobserve(element: Element): void {
    this.observed.delete(element);
  }

  disconnect(): void {
    this.observed.clear();
  }
}

function enter(field: HTMLInputElement | HTMLTextAreaElement, value: string): void {
  field.value = value;
  field.dispatchEvent(new Event('input', { bubbles: true }));
  flushSync();
}

describe('message composer form', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount> | undefined;
  let api: FakeApi;
  let created: Created;
  let screenWork: ScreenWork;
  const translator = new Translator('en');
  const reload = vi.fn<() => void>();

  function root(): HTMLElement {
    const section = target.querySelector<HTMLElement>('#create');
    if (!section) {
      throw new Error('missing composer');
    }
    return section;
  }
  function text(key: MessageKey, parameters?: Parameters<Translator['translate']>[1]): string {
    return translator.translate(key, parameters);
  }
  function button(label: string): HTMLButtonElement {
    const found = [...root().querySelectorAll('button')].find(
      (element) => element.getAttribute('aria-label') === label || element.textContent.trim() === label,
    );
    if (!found) {
      throw new Error(`missing button ${label}`);
    }
    return found;
  }
  function chip(menu: string): HTMLButtonElement {
    const found = root().querySelector<HTMLButtonElement>(`button[aria-controls$="-${menu}"]`);
    if (!found) {
      throw new Error(`missing menu ${menu}`);
    }
    return found;
  }
  function textarea(): HTMLTextAreaElement {
    const found = root().querySelector('textarea');
    if (!found) {
      throw new Error('missing text field');
    }
    return found;
  }
  function fileInput(): HTMLInputElement {
    const found = root().querySelector<HTMLInputElement>('input[type="file"]');
    if (!found) {
      throw new Error('missing file input');
    }
    return found;
  }
  function choose(file: File): void {
    const input = fileInput();
    Object.defineProperty(input, 'files', { value: [file], configurable: true });
    input.dispatchEvent(new Event('change', { bubbles: true }));
    flushSync();
  }
  function protection(): HTMLButtonElement {
    const found = root().querySelector<HTMLButtonElement>('button[role="switch"]');
    if (!found) {
      throw new Error('missing protection switch');
    }
    return found;
  }
  function saveTracking(): void {
    const label = root().querySelector(`input[placeholder="${CSS.escape(text('created.track.placeholder'))}"]`);
    const save = label?.closest('form')?.querySelector('button');
    if (!save) {
      throw new Error('missing tracking form');
    }
    save.click();
  }
  function submitButton(): HTMLButtonElement {
    const found = root().querySelector<HTMLButtonElement>('button[type="submit"]');
    if (!found) {
      throw new Error('missing submit');
    }
    return found;
  }
  function drag(type: string, transfer: object, relatedTarget: EventTarget | null = null): Event {
    const event = Object.assign(new MouseEvent(type, { bubbles: true, cancelable: true, relatedTarget }), {
      dataTransfer: transfer,
    });
    textarea().parentElement?.dispatchEvent(event);
    flushSync();
    return event;
  }
  function dropZone(): Element | null | undefined {
    return textarea().parentElement?.lastElementChild;
  }
  function finishAnimations(): void {
    for (const element of root().querySelectorAll('*')) {
      for (const animation of element.getAnimations()) {
        animation.finish();
      }
    }
  }
  async function show(): Promise<void> {
    component = mount(ComposerSection, { target });
    flushSync();
    await vi.waitFor(() => expect(submitButton().disabled).toBe(false));
  }

  beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    api = fakeApi();
    screenWork = new ScreenWork();
    vi.mocked(createApi).mockReturnValue(api);
    vi.mocked(getTranslator).mockReturnValue(translator);
    vi.mocked(getScreenWork).mockReturnValue(screenWork);
    vi.mocked(getSessionSentStore).mockReturnValue(sessionSentStore());
    vi.mocked(browserSentStore).mockReturnValue({ ...sessionSentStore(), close: vi.fn<() => void>() });
    vi.mocked(rememberSent).mockResolvedValue(true);
    created = {
      id: 'AAAAAAAAAAAAAAAAAAAAAA',
      link: 'https://nylzo.test/r#3.0.AAAAAAAAAAAAAAAAAAAAAA.key.share',
      expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
      manageKey: await requestKey(new Uint8Array(32).fill(7)),
    };
    vi.mocked(createMessage).mockResolvedValue(created);
    vi.stubGlobal('location', { origin: 'https://nylzo.test', reload });
    vi.stubGlobal('IntersectionObserver', RecordingObserver);
    vi.stubGlobal('navigation', new EventTarget());
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn<(text: string) => Promise<void>>() } });
    target = document.createElement('div');
    document.body.append(target);
  });
  afterEach(async () => {
    if (component) {
      await unmount(component);
    }
    component = undefined;
    target.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('offers a retry when the instance limits cannot load', async () => {
    const instance = vi.spyOn(api, 'instance').mockRejectedValueOnce(new Error('offline'));
    component = mount(ComposerSection, { target });
    await vi.waitFor(() => expect(root().textContent).toContain(text('failure.instance')));
    expect(submitButton().disabled).toBe(true);
    button(text('action.retry')).click();
    await vi.waitFor(() => expect(submitButton().disabled).toBe(false));
    expect(instance).toHaveBeenCalledTimes(2);
  });

  it('counts the text near its limit and marks an oversized text', async () => {
    await show();
    const counter = () => textarea().parentElement?.querySelector('span[aria-hidden="true"].shrink-0');
    enter(textarea(), 'short');
    expect(counter()?.textContent.trim()).toBe('');
    enter(textarea(), 'x'.repeat(102_401));
    expect(counter()?.textContent.trim()).not.toBe('');
    submitButton().click();
    await tick();
    expect(textarea().getAttribute('aria-invalid')).toBe('true');
    expect(textarea().getAttribute('aria-describedby')).not.toBeNull();
    expect(createMessage).not.toHaveBeenCalled();
  });

  it('chooses the lifetime and the number of opens from their menus', async () => {
    await show();
    const ttl = chip('ttl');
    ttl.click();
    flushSync();
    expect(ttl.getAttribute('aria-expanded')).toBe('true');
    const options = [...root().querySelectorAll<HTMLButtonElement>('[aria-pressed]')];
    const unpicked = options.find((option) => option.getAttribute('aria-pressed') === 'false');
    unpicked?.click();
    flushSync();
    expect(unpicked?.getAttribute('aria-pressed')).toBe('true');
    expect(ttl.getAttribute('aria-expanded')).toBe('false');
    expect(document.activeElement).toBe(ttl);
    expect(ttl.textContent).toContain(unpicked?.textContent.trim());

    const opens = chip('opens');
    opens.click();
    flushSync();
    const fewer = button(text('compose.opens.less'));
    const more = button(text('compose.opens.more'));
    expect(fewer.getAttribute('aria-disabled')).toBe('true');
    fewer.click();
    more.click();
    more.click();
    flushSync();
    expect(opens.textContent).toContain(translator.plural('compose.opens', 3));
    fewer.click();
    flushSync();
    expect(opens.textContent).toContain(translator.plural('compose.opens', 2));
    for (let index = 0; index < 10; index++) {
      more.click();
    }
    flushSync();
    expect(more.getAttribute('aria-disabled')).toBe('true');
    expect(opens.textContent).toContain(translator.plural('compose.opens', 10));
    opens.parentElement
      ?.querySelector('[role="dialog"], [id$="-opens"]')
      ?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    flushSync();
    expect(opens.getAttribute('aria-expanded')).toBe('false');
  });

  it('asks for a passphrase when protection is on and marks the menu until it is fixed', async () => {
    await show();
    enter(textarea(), 'synthetic message');
    const security = chip('security');
    security.click();
    flushSync();
    protection().click();
    await vi.waitFor(() => expect(root().querySelector<HTMLInputElement>('input[type="text"]')?.value).not.toBe(''));
    const passphrase = root().querySelector<HTMLInputElement>('input[type="text"]');
    if (!passphrase) {
      throw new Error('missing passphrase field');
    }
    expect(security.textContent).toContain(text('compose.passphrase.chip'));
    enter(passphrase, '');
    submitButton().click();
    await vi.waitFor(() => expect(passphrase.getAttribute('aria-invalid')).toBe('true'));
    expect(passphrase.getAttribute('aria-describedby')).not.toBeNull();
    const outside = document.createElement('button');
    document.body.append(outside);
    passphrase.dispatchEvent(new FocusEvent('focusout', { bubbles: true, relatedTarget: outside }));
    flushSync();
    outside.remove();
    expect(security.getAttribute('aria-expanded')).toBe('false');
    expect(security.className).not.toBe(chip('ttl').className);
    protection().click();
    flushSync();
    expect(passphrase.getAttribute('aria-invalid')).toBe('false');
    expect(security.className).toBe(chip('ttl').className);
    expect(security.textContent).toContain(text('compose.passphrase.chip.off'));
    expect(passphrase.disabled).toBe(true);
  });

  it('shows a chosen, an oversized and a dropped file and clears it', async () => {
    await show();
    choose(new File(['hello'], 'notes.txt'));
    expect(root().textContent).toContain('notes.txt');
    expect(root().textContent).toContain(formatBytes(5, 'en'));
    expect(fileInput().getAttribute('aria-invalid')).toBe('false');
    choose(new File([new Uint8Array(5_242_881)], 'huge.bin'));
    expect(fileInput().getAttribute('aria-invalid')).toBe('true');
    expect(root().textContent).toContain(
      text('compose.file.over', { size: formatBytes(5_242_881, 'en'), limit: formatBytes(5_242_880, 'en') }),
    );
    button(text('create.file.clear')).click();
    flushSync();
    expect(fileInput().getAttribute('aria-invalid')).toBe('false');

    const dropped = new File(['dropped'], 'dropped.txt');
    const files = { types: ['Files'], files: [dropped], items: [{ webkitGetAsEntry: () => ({ isDirectory: false }) }] };
    expect(drag('dragover', { types: ['text/plain'] }).defaultPrevented).toBe(false);
    expect(drag('dragenter', files).defaultPrevented).toBe(true);
    expect(dropZone()?.className).toContain('opacity-100');
    drag('dragleave', files, textarea());
    expect(dropZone()?.className).toContain('opacity-100');
    drag('dragleave', files, document.body);
    expect(dropZone()?.className).toContain('opacity-0');
    expect(drag('drop', { types: ['text/plain'] }).defaultPrevented).toBe(false);
    expect(drag('drop', files).defaultPrevented).toBe(true);
    expect(root().textContent).toContain('dropped.txt');
    const folder = { ...files, items: [{ webkitGetAsEntry: () => ({ isDirectory: true }) }] };
    drag('drop', folder);
    expect(fileInput().getAttribute('aria-invalid')).toBe('true');
  });

  it('seals a message, keeps it in the tab when asked and starts another', async () => {
    await show();
    const gate = Promise.withResolvers<Created>();
    vi.mocked(createMessage).mockReturnValueOnce(gate.promise);
    enter(textarea(), 'synthetic message');
    chip('security').click();
    flushSync();
    protection().click();
    await vi.waitFor(() => expect(root().querySelector<HTMLInputElement>('input[type="text"]')?.value).not.toBe(''));
    submitButton().click();
    await vi.waitFor(() => expect(createMessage).toHaveBeenCalledOnce());
    flushSync();
    expect(screenWork.busy).toBe(true);
    expect(textarea().readOnly).toBe(true);
    expect(root().querySelector('[inert][aria-hidden="true"].invisible')?.textContent).toContain(
      text('created.sub.passphrase'),
    );
    const dropped = { types: ['Files'], files: [new File(['x'], 'late.txt')], items: [] };
    expect(drag('dragover', dropped).defaultPrevented).toBe(false);
    drag('drop', dropped);
    expect(root().textContent).not.toContain('late.txt');
    expect(root().querySelector('[role="status"][aria-live="polite"]')?.textContent).not.toBe('');
    gate.resolve(created);
    await vi.advanceTimersByTimeAsync(2000);
    flushSync();
    expect(screenWork.busy).toBe(false);
    expect(root().querySelector<HTMLInputElement>('input[readonly]')?.value).toBe(created.link);
    vi.mocked(browserSentStore).mockClear();
    button(text('created.track.title')).click();
    await tick();
    saveTracking();
    await vi.advanceTimersByTimeAsync(2000);
    flushSync();
    expect(rememberSent).toHaveBeenCalledOnce();
    const remembered = () => root().querySelector('p[role="status"]')?.parentElement?.className;
    finishAnimations();
    await vi.waitFor(() => expect(remembered()).toContain('opacity-100'));
    expect(root().textContent).toContain(text('created.remembered'));
    button(text('created.another')).click();
    await vi.advanceTimersByTimeAsync(500);
    flushSync();
    expect(textarea().readOnly).toBe(false);
    expect(textarea().value).toBe('');
  });

  it('says when the sent message could not be kept and keeps it in the tab on a second try', async () => {
    vi.mocked(rememberSent).mockResolvedValueOnce(false);
    await show();
    enter(textarea(), 'synthetic message');
    submitButton().click();
    await vi.advanceTimersByTimeAsync(2000);
    flushSync();
    button(text('created.track.title')).click();
    await tick();
    saveTracking();
    await vi.waitFor(() => expect(root().textContent).toContain(text('created.not_remembered')));
    saveTracking();
    await vi.advanceTimersByTimeAsync(2000);
    await vi.waitFor(() => expect(root().textContent).toContain(text('created.remembered.session')));
    expect(vi.mocked(rememberSent).mock.lastCall?.[0]).not.toBe(vi.mocked(rememberSent).mock.calls[0]?.[0]);
  });

  it('closes menus on an outside click, forgets the passphrase on unload and reloads a restored page', async () => {
    await show();
    chip('ttl').click();
    flushSync();
    document.body.click();
    flushSync();
    expect(chip('ttl').getAttribute('aria-expanded')).toBe('false');
    chip('security').click();
    flushSync();
    protection().click();
    await vi.waitFor(() => expect(root().querySelector<HTMLInputElement>('input[type="text"]')?.value).not.toBe(''));
    dispatchEvent(new Event('beforeunload'));
    flushSync();
    expect(root().querySelector<HTMLInputElement>('input[type="text"]')?.value).toBe('');
    dispatchEvent(Object.assign(new Event('pageshow'), { persisted: true }));
    expect(reload).toHaveBeenCalledOnce();
    choose(new File(['hello'], 'notes.txt'));
    dispatchEvent(new Event('pagehide'));
    flushSync();
    expect(root().textContent).not.toContain('notes.txt');
  });
});
