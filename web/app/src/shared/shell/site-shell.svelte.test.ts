import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { BuildInfo } from '../build-info.ts';
import type { Language } from '../language/language.ts';
import { getTranslator, Translator } from '../language/translator.svelte.ts';
import PageSkeleton from './PageSkeleton.svelte';
import { REPOSITORY_URL } from './repository.ts';
import SiteFooter from './SiteFooter.svelte';
import SiteHeader from './SiteHeader.svelte';

const build = vi.hoisted((): { info: BuildInfo } => ({
  info: { version: '1.2.3', commit: 'abcdef1', date: '2026-11-01', dirty: false },
}));

vi.mock(import('../language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);
vi.mock(import('../build-info.ts'), (original) =>
  original().then((actual) => ({
    ...actual,
    get BUILD_INFO() {
      return build.info;
    },
  })),
);

describe('site shell', () => {
  const onlanguage = vi.fn<(language: Language) => void>();
  let component: ReturnType<typeof mount> | undefined;
  let target: HTMLElement;

  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(getTranslator).mockReturnValue(new Translator('cs'));
    build.info = { version: '1.2.3', commit: 'abcdef1', date: '2026-11-01', dirty: false };
    target = document.createElement('div');
    document.body.append(target);
  });

  afterEach(async () => {
    if (component) {
      await unmount(component);
      component = undefined;
    }
    document.body.replaceChildren();
  });

  function links(): readonly HTMLAnchorElement[] {
    return [...target.querySelectorAll('a')];
  }

  function hrefs(): readonly (string | null)[] {
    return links().map((link) => link.getAttribute('href'));
  }

  function current(): readonly (string | null)[] {
    return links()
      .filter((link) => link.getAttribute('aria-current') === 'page')
      .map((link) => link.getAttribute('href'));
  }

  function languageMenu(): Element | null {
    return target.querySelector('[data-menu]');
  }

  function showHeader(props: {
    readonly variant: 'home' | 'read' | 'page' | 'status';
    readonly current?: string;
  }): void {
    component = mount(SiteHeader, { target, props: { ...props, onlanguage } });
    flushSync();
  }

  function showFooter(props: { readonly compact: boolean; readonly current: string }): void {
    component = mount(SiteFooter, { target, props: { ...props, onlanguage } });
    flushSync();
  }

  describe('header', () => {
    it('links the home sections and the sent messages on the home page', () => {
      showHeader({ variant: 'home' });

      expect(hrefs()).toEqual([
        '#create',
        '#how-it-works',
        '#security',
        '#resilience',
        '#self-host',
        '#comparison',
        '/sent',
        REPOSITORY_URL,
      ]);
      expect(languageMenu()?.classList.contains('max-sm:hidden')).toBe(false);
    });

    it('leads a page back home in the reader language and marks the current page', () => {
      showHeader({ variant: 'page', current: 'sent' });

      expect(hrefs()).toEqual(['/cs/', '/sent', '/cs/', REPOSITORY_URL]);
      expect(current()).toEqual(['/sent']);
      expect(languageMenu()?.classList.contains('max-sm:hidden')).toBe(true);
    });

    it('offers writing a new message but not the sent list while reading', () => {
      showHeader({ variant: 'read' });

      expect(hrefs()).toEqual(['/cs/', '/cs/', REPOSITORY_URL]);
      expect(current()).toEqual([]);
    });

    it('shows only the logo and the repository on a status page without a language menu', () => {
      component = mount(SiteHeader, { target, props: { variant: 'status' } });
      flushSync();

      expect(hrefs()).toEqual(['/cs/', REPOSITORY_URL]);
      expect(languageMenu()).toBeNull();
    });
  });

  describe('footer', () => {
    it('groups the site links, marks the current page and links the release', () => {
      showFooter({ compact: false, current: 'sent' });

      expect(target.querySelectorAll('nav')).toHaveLength(3);
      expect(hrefs()).toContain('/cs/#how-it-works');
      expect(hrefs()).toContain(`${REPOSITORY_URL}/releases/tag/v1.2.3`);
      expect(current()).toEqual(['/sent']);
      expect(languageMenu()).not.toBeNull();
    });

    it('marks the terms as the current page', () => {
      showFooter({ compact: false, current: 'terms' });

      expect(current()).toEqual(['/terms']);
    });

    it('shows a version without a source as plain text', () => {
      build.info = { version: 'dev', commit: 'unknown', date: '', dirty: true };
      showFooter({ compact: false, current: 'create' });

      expect(hrefs().some((href) => href?.startsWith(`${REPOSITORY_URL}/releases`))).toBe(false);
      expect(hrefs().some((href) => href?.startsWith(`${REPOSITORY_URL}/commit`))).toBe(false);
      expect([...target.querySelectorAll('span')].some((span) => span.textContent === 'dev')).toBe(true);
    });

    it('keeps only the essential links when compact', () => {
      showFooter({ compact: true, current: 'read' });

      expect(target.querySelector('nav')).toBeNull();
      expect(hrefs()).toEqual(['/cs/#how-it-works', REPOSITORY_URL, `${REPOSITORY_URL}/releases/tag/v1.2.3`]);
      expect(languageMenu()).not.toBeNull();
    });
  });

  it('shows a busy page skeleton while the app loads', () => {
    component = mount(PageSkeleton, { target });
    flushSync();

    expect(target.querySelector('[aria-busy="true"]')).not.toBeNull();
  });
});
