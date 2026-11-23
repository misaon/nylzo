import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { loadCatalog } from '../shared/language/catalog.ts';
import type { MessageKey } from '../shared/language/catalog.ts';
import type { Language } from '../shared/language/language.ts';
import { Translator } from '../shared/language/translator.svelte.ts';
import type { ScreenWork } from '../shared/navigation/screen-work.ts';
import type { SessionSentStore } from '../shared/sent-records/session-sent-store.ts';
import App from './App.svelte';

const shell = vi.hoisted(() => {
  const state: { tracked?: SessionSentStore; work?: ScreenWork; agentsFail: boolean } = { agentsFail: false };
  return state;
});

vi.mock(import('../shared/language/catalog.ts'), async (original) => {
  const actual = await original();
  return { ...actual, loadCatalog: vi.fn<typeof actual.loadCatalog>(actual.loadCatalog) };
});
vi.mock(import('../shared/sent-records/session-sent-store.ts'), async (original) => {
  const actual = await original();
  return {
    ...actual,
    setSessionSentStore: (store: SessionSentStore) => {
      shell.tracked = store;
      return actual.setSessionSentStore(store);
    },
  };
});
vi.mock(import('../shared/navigation/screen-work.ts'), async (original) => {
  const actual = await original();
  return {
    ...actual,
    setScreenWork: (work: ScreenWork) => {
      shell.work = work;
      return actual.setScreenWork(work);
    },
  };
});
vi.mock('../features/agents/Agents.svelte', async (original) => {
  const { default: Agents } = await original<typeof import('../features/agents/Agents.svelte')>();
  return {
    default: (...parameters: Parameters<typeof Agents>) => {
      if (shell.agentsFail) {
        throw new Error('the agents screen failed');
      }
      return Agents(...parameters);
    },
  };
});

interface Navigation {
  readonly url: string;
  readonly navigationType?: NavigationType;
  readonly canIntercept?: boolean;
  readonly hashChange?: boolean;
  readonly downloadRequest?: string | null;
  readonly formData?: FormData | null;
  readonly info?: unknown;
  readonly cancelable?: boolean;
}

type Intercept = (options: NavigationInterceptOptions) => void;

function translated(language: Language, key: MessageKey): string {
  return new Translator(language).translate(key);
}

function tracked(): SessionSentStore {
  if (!shell.tracked) {
    throw new Error('the app set no session store');
  }
  return shell.tracked;
}

function screenWork(): ScreenWork {
  if (!shell.work) {
    throw new Error('the app set no screen work');
  }
  return shell.work;
}

function holdMessages(): void {
  vi.spyOn(tracked(), 'held').mockReturnValue(true);
}

async function intercepted(intercept: ReturnType<typeof vi.fn<Intercept>>): Promise<void> {
  expect(intercept).toHaveBeenCalledOnce();
  await intercept.mock.calls[0]?.[0].handler?.();
  flushSync();
}

function choose(language: Language): void {
  document.querySelector<HTMLElement>(`li[role="option"][lang="${language}"]`)?.click();
  flushSync();
}

function unloadPrevented(): boolean {
  const event = new Event('beforeunload', { cancelable: true });
  dispatchEvent(event);
  return event.defaultPrevented;
}

function drag(type: 'dragover' | 'drop', source: EventTarget, types?: readonly string[]): boolean {
  const event = Object.assign(new Event(type, { bubbles: true, cancelable: true }), {
    dataTransfer: types ? { types } : null,
  });
  source.dispatchEvent(event);
  return event.defaultPrevented;
}

describe('app shell', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount> | undefined;
  let navigation: EventTarget;

  function show(address: string, language: Language = 'en'): void {
    history.replaceState(null, '', address);
    const { pathname } = new URL(address, location.origin);
    component = mount(App, { target, props: { start: { path: pathname, language } } });
    flushSync();
  }

  function navigate({ cancelable = true, ...options }: Navigation): ReturnType<typeof vi.fn<Intercept>> {
    const intercept = vi.fn<Intercept>();
    const event = Object.assign(new Event('navigate', { cancelable }), {
      canIntercept: true,
      hashChange: false,
      downloadRequest: null,
      formData: null,
      navigationType: 'push',
      ...options,
      destination: { url: new URL(options.url, location.origin).href, key: 'destination' },
      intercept,
    });
    navigation.dispatchEvent(event);
    flushSync();
    return intercept;
  }

  beforeEach(() => {
    shell.agentsFail = false;
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>(async () => Promise.withResolvers<Response>().promise),
    );
    navigation = new EventTarget();
    target = document.createElement('div');
    document.body.append(target);
  });
  afterEach(async () => {
    if (component) {
      await unmount(component);
      component = undefined;
    }
    target.remove();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    vi.mocked(loadCatalog).mockClear();
    Reflect.deleteProperty(document, 'startViewTransition');
    document.documentElement.lang = '';
    localStorage.clear();
    history.replaceState(null, '', '/');
  });

  it.each([
    ['/', 'seo.title'],
    ['/agents', 'title.agents'],
    ['/terms', 'title.terms'],
    ['/sent', 'title.sent'],
    ['/transparency', 'title.transparency'],
    ['/nothing', 'title.notFound'],
    ['/r', 'title.read'],
  ] as const)('shows the screen of %s', (path, title) => {
    show(path);
    expect(document.title).toBe(translated('en', title));
  });

  it('shows a notice in place of a screen that fails to render', () => {
    shell.agentsFail = true;
    show('/agents');
    expect(target.querySelector('main [role="alert"] p')?.textContent).toBe(translated('en', 'app.crashed'));
  });

  it('forgets the messages tracked in this tab when the page goes away', () => {
    show('/sent');
    const clear = vi.spyOn(tracked(), 'clear').mockResolvedValue();
    globalThis.dispatchEvent(new Event('pagehide'));
    expect(clear).toHaveBeenCalledOnce();
  });

  describe('history', () => {
    it('ignores a history step that stays on the shown address', () => {
      const reload = vi.spyOn(location, 'reload').mockReturnValue();
      show('/terms');
      globalThis.dispatchEvent(new Event('popstate'));
      expect(reload).not.toHaveBeenCalled();
    });

    it('loads the page again when history returns to another screen', () => {
      const reload = vi.spyOn(location, 'reload').mockReturnValue();
      show('/terms');
      history.replaceState(null, '', '/sent');
      globalThis.dispatchEvent(new Event('popstate'));
      expect(reload).toHaveBeenCalledOnce();
    });

    it('keeps the shown language when history returns to the home page of another language', () => {
      const reload = vi.spyOn(location, 'reload').mockReturnValue();
      show('/cs/', 'cs');
      history.replaceState(null, '', '/?from=link#faq');
      globalThis.dispatchEvent(new Event('popstate'));
      expect(`${location.pathname}${location.search}${location.hash}`).toBe('/cs/?from=link#faq');
      expect(reload).not.toHaveBeenCalled();
    });
  });

  describe('navigation', () => {
    beforeEach(() => {
      vi.stubGlobal('navigation', navigation);
    });

    it('switches between screens inside the document while this tab tracks messages and focuses the new page', async () => {
      show('/terms');
      holdMessages();
      const intercept = navigate({ url: '/agents' });
      await intercepted(intercept);
      expect(document.title).toBe(translated('en', 'title.agents'));
      expect(intercept.mock.calls[0]?.[0].focusReset).toBe('manual');
      expect(document.activeElement).toBe(target.querySelector('main'));
      expect(component).toBeDefined();
    });

    it.each<{ readonly reason: string; readonly from: string; readonly navigation: Navigation; readonly busy?: true }>([
      { reason: 'a screen is still at work', from: '/terms', navigation: { url: '/agents' }, busy: true },
      {
        reason: 'the browser cannot intercept it',
        from: '/terms',
        navigation: { url: '/agents', canIntercept: false },
      },
      { reason: 'only the fragment changes', from: '/terms', navigation: { url: '/terms#top', hashChange: true } },
      { reason: 'it downloads a file', from: '/terms', navigation: { url: '/agents', downloadRequest: 'guide.md' } },
      { reason: 'it submits a form', from: '/terms', navigation: { url: '/agents', formData: new FormData() } },
      { reason: 'it reloads the page', from: '/terms', navigation: { url: '/terms', navigationType: 'reload' } },
      { reason: 'it leaves a read page', from: '/r', navigation: { url: '/terms' } },
      { reason: 'it goes to an unknown address', from: '/terms', navigation: { url: '/nothing' } },
    ])('leaves a navigation to the browser when $reason', ({ from, navigation: options, busy }) => {
      show(from);
      holdMessages();
      if (busy) {
        screenWork().watch(() => true);
      }
      const title = document.title;
      expect(navigate(options)).not.toHaveBeenCalled();
      expect(document.title).toBe(title);
    });

    it.each([
      { unsaved: 'draft', navigationType: 'push' },
      { unsaved: 'link', navigationType: 'traverse' },
    ] as const)(
      'asks before a switch drops an unsaved $unsaved and goes on only once confirmed',
      async ({ unsaved, navigationType }) => {
        const navigateAgain = vi.fn<(url: string, options: NavigationNavigateOptions) => void>();
        const traverseAgain = vi.fn<(key: string, options: NavigationOptions) => void>();
        Object.assign(navigation, { navigate: navigateAgain, traverseTo: traverseAgain });
        show('/terms');
        holdMessages();
        screenWork().guard(() => unsaved);
        const answer = async (index: number) => {
          const dialog = document.querySelector('dialog');
          expect(dialog?.textContent).toContain(translated('en', `leave.${unsaved}.title`));
          dialog?.querySelectorAll('button')[index]?.click();
          await tick();
          flushSync();
        };

        expect(navigate({ url: '/agents', navigationType })).not.toHaveBeenCalled();
        await answer(0);
        expect(document.querySelector('dialog')).toBeNull();
        expect([navigateAgain, traverseAgain].flatMap((repeat) => repeat.mock.calls)).toEqual([]);

        expect(navigate({ url: '/agents', navigationType })).not.toHaveBeenCalled();
        await answer(1);
        const repeated = navigationType === 'push' ? navigateAgain : traverseAgain;
        expect(repeated).toHaveBeenCalledOnce();
        const info: unknown = repeated.mock.calls[0]?.[1].info;
        await intercepted(navigate({ url: '/agents', navigationType, info }));
        expect(document.title).toBe(translated('en', 'title.agents'));
      },
    );

    it('drops the question when the browser leaves without letting the page ask', async () => {
      show('/terms');
      holdMessages();
      screenWork().guard(() => 'draft');
      navigate({ url: '/agents' });
      expect(document.querySelector('dialog')).not.toBeNull();
      await intercepted(navigate({ url: '/agents', cancelable: false }));
      expect(document.querySelector('dialog')).toBeNull();
      expect(document.title).toBe(translated('en', 'title.agents'));
    });

    it('asks before unload while work is unsaved, and does not ask again when WebKit then fires navigate', async () => {
      show('/terms');
      holdMessages();
      expect(unloadPrevented()).toBe(false);
      screenWork().guard(() => 'link');
      expect(unloadPrevented()).toBe(true);
      await intercepted(navigate({ url: '/agents' }));
      expect(document.querySelector('dialog')).toBeNull();
    });

    it('leaves a navigation to the browser while this tab tracks no message', () => {
      show('/terms');
      expect(navigate({ url: '/agents' })).not.toHaveBeenCalled();
    });

    it('keeps the shown language when going back to the home page of another language', async () => {
      show('/cs/', 'cs');
      holdMessages();
      const intercept = navigate({ url: '/', navigationType: 'traverse' });
      history.replaceState(null, '', '/#faq');
      await intercepted(intercept);
      expect(`${location.pathname}${location.hash}`).toBe('/cs/#faq');
    });

    it('stops listening for navigation once the app is gone', async () => {
      show('/terms');
      holdMessages();
      if (component) {
        await unmount(component);
        component = undefined;
      }
      expect(navigate({ url: '/agents' })).not.toHaveBeenCalled();
    });
  });

  describe('file drops', () => {
    it.each(['dragover', 'drop'] as const)('keeps the page when a file is dropped outside a drop zone (%s)', (type) => {
      show('/');
      expect(drag(type, document.body, ['Files'])).toBe(true);
      const text = document.createElement('input');
      target.append(text);
      expect(drag(type, text, ['Files'])).toBe(true);
    });

    it('lets a file input take a dropped file', () => {
      show('/');
      const input = document.createElement('input');
      input.type = 'file';
      target.append(input);
      expect(drag('drop', input, ['Files'])).toBe(false);
    });

    it('lets dragged text and drags without data through', () => {
      show('/');
      expect(drag('drop', document.body, ['text/plain'])).toBe(false);
      expect(drag('dragover', document.body)).toBe(false);
    });
  });

  describe('language', () => {
    it('switches the language of a screen in place and remembers it', async () => {
      show('/terms');
      choose('cs');
      await vi.waitFor(() => {
        expect(document.documentElement.lang).toBe('cs');
      });
      flushSync();
      expect(document.title).toBe(translated('cs', 'title.terms'));
      expect(localStorage.getItem('nylzo:lang')).toBe('cs');
      expect(location.pathname).toBe('/terms');
    });

    it('moves the home page to the address of the chosen language without intercepting its own change', async () => {
      vi.stubGlobal('navigation', navigation);
      show('/#faq');
      holdMessages();
      const intercepts: ReturnType<typeof vi.fn<Intercept>>[] = [];
      const replaceState = history.replaceState.bind(history);
      vi.spyOn(history, 'replaceState').mockImplementation(
        (state: unknown, unused: string, url?: string | URL | null) => {
          intercepts.push(navigate({ url: String(url), navigationType: 'replace' }));
          replaceState(state, unused, url);
        },
      );
      choose('de');
      await vi.waitFor(() => {
        expect(`${location.pathname}${location.hash}`).toBe('/de/#faq');
      });
      expect(intercepts).toHaveLength(1);
      expect(intercepts[0]).not.toHaveBeenCalled();
      expect(localStorage.getItem('nylzo:lang')).toBe('de');
    });

    it('animates the switch with a view transition where the browser has one', async () => {
      const startViewTransition = vi.fn<(update: () => Promise<void>) => void>((update) => {
        void update();
      });
      Object.defineProperty(document, 'startViewTransition', { configurable: true, value: startViewTransition });
      show('/terms');
      choose('fr');
      await vi.waitFor(() => {
        expect(document.documentElement.lang).toBe('fr');
      });
      expect(startViewTransition).toHaveBeenCalledOnce();
    });

    it('does nothing when the shown language is chosen again', async () => {
      show('/terms');
      choose('en');
      await tick();
      expect(loadCatalog).not.toHaveBeenCalled();
      expect(localStorage.getItem('nylzo:lang')).toBeNull();
    });

    it('keeps only the later of two quick choices', async () => {
      const { promise: german, resolve } = Promise.withResolvers<undefined>();
      vi.mocked(loadCatalog).mockReturnValueOnce(german);
      show('/terms');
      choose('de');
      choose('fr');
      await vi.waitFor(() => {
        expect(document.documentElement.lang).toBe('fr');
      });
      resolve(undefined);
      await german;
      await tick();
      expect(document.documentElement.lang).toBe('fr');
      expect(localStorage.getItem('nylzo:lang')).toBe('fr');
    });

    it.each<{
      readonly reason: string;
      readonly from: string;
      readonly reloads: boolean;
      readonly held?: true;
      readonly offline?: true;
    }>([
      { reason: 'loads a screen without a message again', from: '/terms', reloads: true },
      { reason: 'keeps the page while offline', from: '/terms', reloads: false, offline: true },
      { reason: 'keeps a page that tracks messages', from: '/terms', reloads: false, held: true },
      { reason: 'keeps the home page', from: '/', reloads: false },
      { reason: 'keeps a read page', from: '/r', reloads: false },
    ])('saves a choice whose catalog is gone and $reason', async ({ from, reloads, held, offline }) => {
      const reload = vi.spyOn(location, 'reload').mockReturnValue();
      vi.mocked(loadCatalog).mockRejectedValueOnce(new Error('catalog removed'));
      if (offline) {
        vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false);
      }
      show(from);
      if (held) {
        holdMessages();
      }
      choose('cs');
      await vi.waitFor(() => {
        expect(localStorage.getItem('nylzo:lang')).toBe('cs');
      });
      expect(reload).toHaveBeenCalledTimes(reloads ? 1 : 0);
      expect(document.documentElement.lang).toBe('');
    });
  });
});
