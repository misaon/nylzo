// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { languageDetails } from '../shared/language/language.ts';
import App from './App.svelte';

const startup = vi.hoisted(() => ({
  mount: vi.fn<typeof import('svelte').mount>(),
  hydrate: vi.fn<typeof import('svelte').hydrate>(),
  prepareWorker: vi.fn<typeof import('../shared/kdf/worker-source.ts').prepareWorker>(),
  loadCatalog: vi.fn<typeof import('../shared/language/catalog.ts').loadCatalog>(),
  watchBrowserVisit: vi.fn<typeof import('../shared/visits/visits.ts').watchBrowserVisit>(),
  keepPurgingSentRecords: vi.fn<typeof import('../shared/sent-records/sent-store.ts').keepPurgingSentRecords>(),
  forgetRetiredRecovery: vi.fn<typeof import('./retired-recovery.ts').forgetRetiredRecovery>(),
  loadFont: vi.fn<(font: string, sample: string) => Promise<unknown>>(),
  disconnect: vi.fn<() => void>(),
  painted: false,
}));

vi.mock('svelte', async (original) => ({
  ...(await original<typeof import('svelte')>()),
  mount: startup.mount,
  hydrate: startup.hydrate,
}));
vi.mock('./App.svelte', () => ({ default: vi.fn<() => void>() }));
vi.mock(import('../shared/kdf/worker-source.ts'), async (original) => ({
  ...(await original()),
  prepareWorker: startup.prepareWorker,
}));
vi.mock(import('../shared/language/catalog.ts'), async (original) => ({
  ...(await original()),
  loadCatalog: startup.loadCatalog,
}));
vi.mock(import('../shared/visits/visits.ts'), async (original) => ({
  ...(await original()),
  watchBrowserVisit: startup.watchBrowserVisit,
}));
vi.mock(import('../shared/sent-records/sent-store.ts'), async (original) => ({
  ...(await original()),
  keepPurgingSentRecords: startup.keepPurgingSentRecords,
}));
vi.mock(import('./retired-recovery.ts'), () => ({ forgetRetiredRecovery: startup.forgetRetiredRecovery }));

interface PaintEntries {
  readonly getEntriesByName: (name: string) => readonly unknown[];
}

type PaintCallback = (entries: PaintEntries, observer: PaintObserver) => void;

class PaintObserver {
  readonly #callback: PaintCallback;

  constructor(callback: PaintCallback) {
    this.#callback = callback;
  }

  observe(): void {
    this.#callback({ getEntriesByName: () => [] }, this);
    if (startup.painted) {
      this.#callback({ getEntriesByName: (name) => (name === 'first-contentful-paint' ? [{ name }] : []) }, this);
    }
  }

  disconnect(): void {
    startup.disconnect();
  }
}

const NEVER = Promise.withResolvers<never>().promise;
// The first boot transforms the whole app, which takes seconds on a cold CI runner with coverage.
const STARTUP = { timeout: 15_000 };

function prerenderedHome(): void {
  const main = document.createElement('main');
  main.append(document.createElement('h1'));
  document.body.replaceChildren(main);
}

function prerenderedSkeleton(): void {
  const skeleton = document.createElement('div');
  skeleton.dataset['skeleton'] = '';
  document.body.replaceChildren(skeleton);
}

async function boot(path: string): Promise<void> {
  history.replaceState(null, '', path);
  vi.resetModules();
  await import('./main.ts');
}

async function settled(): Promise<void> {
  await vi.waitFor(async () => {
    await vi.runAllTimersAsync();
    expect(document.documentElement.dataset['ready']).toBe('');
  }, STARTUP);
}

async function failed(): Promise<HTMLElement> {
  return vi.waitFor(async () => {
    await vi.runAllTimersAsync();
    const notice = document.querySelector<HTMLElement>('#start-failure');
    expect(notice).not.toBeNull();
    return notice ?? document.body;
  }, STARTUP);
}

function notices(): number {
  return document.querySelectorAll('#start-failure').length;
}

describe('client startup', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    startup.painted = false;
    startup.loadCatalog.mockResolvedValue();
    startup.prepareWorker.mockResolvedValue();
    startup.keepPurgingSentRecords.mockResolvedValue();
    startup.loadFont.mockResolvedValue([]);
    Object.defineProperty(document, 'fonts', { configurable: true, value: { load: startup.loadFont } });
    vi.stubGlobal('PerformanceObserver', PaintObserver);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    Reflect.deleteProperty(document, 'fonts');
    document.head.replaceChildren();
    document.body.replaceChildren();
    for (const attribute of ['data-fonts', 'data-ready', 'data-skeleton']) {
      document.documentElement.removeAttribute(attribute);
    }
    document.documentElement.lang = '';
    localStorage.clear();
    history.replaceState(null, '', '/');
  });

  it('mounts the app on an empty page in the saved language and keeps browser storage tidy', async () => {
    localStorage.setItem('nylzo:lang', 'de');
    await boot('/sent');
    await settled();
    expect(startup.loadCatalog).toHaveBeenCalledExactlyOnceWith('de');
    expect(document.documentElement.lang).toBe('de');
    expect(document.documentElement.dataset['fonts']).toBe('');
    expect(startup.mount).toHaveBeenCalledExactlyOnceWith(App, {
      target: document.body,
      props: { start: { path: '/sent', language: 'de' } },
    });
    expect(startup.hydrate).not.toHaveBeenCalled();
    expect(startup.prepareWorker).not.toHaveBeenCalled();
    await vi.waitFor(() => {
      expect(startup.watchBrowserVisit).toHaveBeenCalledExactlyOnceWith('sent');
    }, STARTUP);
    expect(startup.forgetRetiredRecovery).toHaveBeenCalledOnce();
    expect(startup.forgetRetiredRecovery.mock.calls[0]?.[0]()).toBe(sessionStorage);
    expect(startup.keepPurgingSentRecords).toHaveBeenCalledOnce();
    const now = startup.keepPurgingSentRecords.mock.calls[0]?.[1];
    expect(now?.()).toBe(Date.now());
  });

  it('replaces the prerendered skeleton with the mounted app', async () => {
    prerenderedSkeleton();
    await boot('/terms');
    await settled();
    expect(startup.mount).toHaveBeenCalledOnce();
    expect(document.body.querySelector('[data-skeleton]')).toBeNull();
    expect(document.documentElement.dataset['skeleton']).toBe('');
  });

  it('hydrates a prerendered home page in its own language once it has painted', async () => {
    localStorage.setItem('nylzo:lang', 'cs');
    startup.painted = true;
    prerenderedHome();
    await boot('/cs/');
    await settled();
    expect(startup.disconnect).toHaveBeenCalledOnce();
    expect(startup.hydrate).toHaveBeenCalledExactlyOnceWith(App, {
      target: document.body,
      props: { start: { path: '/cs/', language: 'cs' } },
    });
    expect(startup.mount).not.toHaveBeenCalled();
    expect(startup.loadFont).toHaveBeenCalledWith(expect.any(String), languageDetails('cs').fontSample);
    expect(startup.prepareWorker).toHaveBeenCalledExactlyOnceWith('low');
    await vi.waitFor(() => {
      expect(startup.watchBrowserVisit).toHaveBeenCalledExactlyOnceWith('create');
    }, STARTUP);
    expect(document.documentElement.lang).toBe('cs');
  });

  it('loads the code of a home page only after its first paint, or a second without one', async () => {
    prerenderedHome();
    await boot('/');
    await vi.advanceTimersByTimeAsync(999);
    expect(document.documentElement.dataset['fonts']).toBe('');
    expect(startup.loadCatalog).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(startup.loadCatalog).toHaveBeenCalledExactlyOnceWith('en');
    await settled();
    expect(startup.disconnect).not.toHaveBeenCalled();
    expect(startup.hydrate).toHaveBeenCalledOnce();
  });

  it('mounts a home page that has no prerendered content without waiting for a paint', async () => {
    await boot('/');
    await settled();
    expect(startup.mount).toHaveBeenCalledOnce();
    expect(startup.disconnect).not.toHaveBeenCalled();
  });

  it('sends a home page to the language saved earlier, keeping the query and fragment', async () => {
    localStorage.setItem('nylzo:lang', 'de');
    const replace = vi.spyOn(location, 'replace').mockReturnValue();
    await boot('/cs/?from=link#faq');
    await vi.runAllTimersAsync();
    expect(replace).toHaveBeenCalledExactlyOnceWith('/de/?from=link#faq');
    expect(startup.loadCatalog).not.toHaveBeenCalled();
    expect(document.documentElement.dataset['fonts']).toBeUndefined();
  });

  it('prepares the key stretching worker for a read page and counts no visit there', async () => {
    await boot('/r');
    await settled();
    expect(startup.prepareWorker).toHaveBeenCalledExactlyOnceWith('auto');
    expect(startup.watchBrowserVisit).not.toHaveBeenCalled();
  });

  it('keeps nothing in browser storage when the local client serves the page', async () => {
    const instance = document.createElement('meta');
    instance.name = 'nylzo-instance';
    instance.content = 'https://nylzo.com';
    document.head.append(instance);
    localStorage.setItem('nylzo:lang', 'de');
    await boot('/agents');
    await settled();
    expect(startup.loadCatalog).toHaveBeenCalledExactlyOnceWith('en');
    expect(startup.forgetRetiredRecovery).not.toHaveBeenCalled();
    expect(startup.keepPurgingSentRecords).not.toHaveBeenCalled();
  });

  it('waits at most one and a half seconds for the fonts', async () => {
    startup.loadFont.mockReturnValue(NEVER);
    await boot('/terms');
    await vi.advanceTimersByTimeAsync(1499);
    expect(document.documentElement.dataset['fonts']).toBeUndefined();
    await vi.advanceTimersByTimeAsync(1);
    expect(document.documentElement.dataset['fonts']).toBe('');
    await settled();
  });

  it('starts when the fonts fail to load', async () => {
    startup.loadFont.mockRejectedValue(new Error('font refused'));
    await boot('/terms');
    await settled();
    expect(startup.mount).toHaveBeenCalledOnce();
  });

  it('keeps the page running when visit counting fails', async () => {
    startup.watchBrowserVisit.mockImplementation(() => {
      throw new Error('visits refused');
    });
    await boot('/agents');
    await settled();
    await vi.waitFor(() => {
      expect(startup.watchBrowserVisit).toHaveBeenCalledOnce();
    }, STARTUP);
    expect(notices()).toBe(0);
  });

  it('offers a retry in the middle of an empty page when the app cannot load', async () => {
    startup.loadCatalog.mockRejectedValue(new Error('catalog missing'));
    const reload = vi.spyOn(location, 'reload').mockReturnValue();
    await boot('/sent');
    const notice = await failed();
    expect(notice.className).toContain('top-1/2');
    expect(notice.querySelector('[role="alert"]')?.textContent).toBe(__NYLZO_START_FAILURE__['en']?.message);
    expect(document.documentElement.dataset['fonts']).toBe('');
    expect(document.documentElement.dataset['ready']).toBeUndefined();
    expect(startup.mount).not.toHaveBeenCalled();
    notice.querySelector('button')?.click();
    expect(reload).toHaveBeenCalledOnce();
  });

  it('asks to open the link again instead of retrying on a read page', async () => {
    startup.loadCatalog.mockRejectedValue(new Error('catalog missing'));
    localStorage.setItem('nylzo:lang', 'fr');
    await boot('/r');
    const notice = await failed();
    expect(notice.querySelector('[role="alert"]')?.textContent).toBe(__NYLZO_START_FAILURE__['fr']?.reopen);
    expect(notice.querySelector('button')).toBeNull();
  });

  it('keeps the notice clear of a prerendered home page and uses its language', async () => {
    startup.loadCatalog.mockRejectedValue(new Error('catalog missing'));
    startup.painted = true;
    prerenderedHome();
    await boot('/pl/');
    const notice = await failed();
    expect(notice.className).toContain('bottom-');
    expect(notice.querySelector('[role="alert"]')?.textContent).toBe(__NYLZO_START_FAILURE__['pl']?.message);
  });

  it('shows the start failure notice only once', async () => {
    startup.loadCatalog.mockRejectedValue(new Error('catalog missing'));
    await boot('/sent');
    await failed();
    await boot('/sent');
    await vi.waitFor(async () => {
      await vi.runAllTimersAsync();
      expect(startup.loadCatalog).toHaveBeenCalledTimes(2);
    }, STARTUP);
    await vi.runAllTimersAsync();
    expect(notices()).toBe(1);
  });

  it('shows no notice in a language the build carries no text for', async () => {
    startup.loadCatalog.mockRejectedValue(new Error('catalog missing'));
    vi.stubGlobal('__NYLZO_START_FAILURE__', {});
    await boot('/sent');
    await vi.waitFor(async () => {
      await vi.runAllTimersAsync();
      expect(startup.loadCatalog).toHaveBeenCalledOnce();
    }, STARTUP);
    await vi.runAllTimersAsync();
    expect(notices()).toBe(0);
    expect(document.documentElement.dataset['fonts']).toBe('');
  });
});
