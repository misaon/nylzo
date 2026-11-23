import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Language } from './language.ts';
import { languageDetails, LANGUAGES } from './language.ts';
import LanguageMenu from './LanguageMenu.svelte';
import { getTranslator, Translator } from './translator.svelte.ts';

vi.mock(import('./translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);

function press(element: HTMLElement, key: string): KeyboardEvent {
  const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true });
  element.dispatchEvent(event);
  flushSync();
  return event;
}

function expanded(trigger: HTMLButtonElement): string | null {
  return trigger.getAttribute('aria-expanded');
}

describe('language menu', () => {
  const onlanguage = vi.fn<(language: Language) => void>();
  let component: ReturnType<typeof mount> | undefined;
  let target: HTMLElement;

  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(getTranslator).mockReturnValue(new Translator('en'));
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

  function show(placement: 'header' | 'footer'): HTMLButtonElement {
    component = mount(LanguageMenu, { target, props: { placement, onlanguage } });
    flushSync();
    const trigger = target.querySelector('button[aria-haspopup]');
    if (!(trigger instanceof HTMLButtonElement)) {
      throw new TypeError('the menu rendered no trigger');
    }
    return trigger;
  }

  function openMenu(): HTMLButtonElement {
    const trigger = show('footer');
    trigger.click();
    flushSync();
    return trigger;
  }

  function options(): readonly HTMLElement[] {
    return [...target.querySelectorAll<HTMLElement>('[role="option"]')];
  }

  function option(language: Language): HTMLElement {
    const found = options().find((candidate) => candidate.lang === language);
    if (!found) {
      throw new Error(`missing option ${language}`);
    }
    return found;
  }

  it('names the current language in the footer', () => {
    const name = languageDetails('en').name;
    const footer = show('footer');

    expect(footer.textContent).toContain(name);
    expect(footer.getAttribute('aria-label')).toContain(name);
  });

  it('shows the current language code in the header', () => {
    const header = show('header');

    expect(header.textContent.trim()).toBe('EN');
    expect(header.getAttribute('aria-label')).toMatch(/^EN, /u);
  });

  it('opens and closes from its trigger and marks the current language', () => {
    const trigger = show('header');
    expect(expanded(trigger)).toBe('false');

    trigger.click();
    flushSync();
    expect(expanded(trigger)).toBe('true');
    expect(option('en').getAttribute('aria-selected')).toBe('true');
    expect(option('en').tabIndex).toBe(0);
    expect(options().filter((candidate) => candidate.tabIndex === 0)).toHaveLength(1);

    trigger.click();
    flushSync();
    expect(expanded(trigger)).toBe('false');
  });

  it('chooses a language by click, Enter or Space and closes', () => {
    const trigger = openMenu();
    const [first, second, third] = LANGUAGES.filter((language) => language !== 'en');
    if (!first || !second || !third) {
      throw new Error('the tests need four languages');
    }

    option(first).click();
    flushSync();
    expect(onlanguage).toHaveBeenLastCalledWith(first);
    expect(expanded(trigger)).toBe('false');

    trigger.click();
    flushSync();
    expect(press(option(second), 'Enter').defaultPrevented).toBe(true);
    expect(onlanguage).toHaveBeenLastCalledWith(second);
    expect(expanded(trigger)).toBe('false');

    trigger.click();
    flushSync();
    press(option(third), ' ');
    expect(onlanguage).toHaveBeenLastCalledWith(third);
    expect(onlanguage).toHaveBeenCalledTimes(3);
  });

  it('moves focus through the options with the arrow, Home and End keys', () => {
    openMenu();
    const list = options();
    const first = list.at(0);
    const second = list.at(1);
    const last = list.at(-1);
    if (!first || !second || !last) {
      throw new Error('the menu needs options');
    }

    press(first, 'ArrowDown');
    expect(document.activeElement).toBe(second);
    press(second, 'ArrowUp');
    expect(document.activeElement).toBe(first);
    press(first, 'ArrowUp');
    expect(document.activeElement).toBe(last);
    press(last, 'ArrowDown');
    expect(document.activeElement).toBe(first);
    press(first, 'End');
    expect(document.activeElement).toBe(last);
    press(last, 'Home');
    expect(document.activeElement).toBe(first);
    expect(press(first, 'a').defaultPrevented).toBe(false);
    expect(document.activeElement).toBe(first);
    expect(onlanguage).not.toHaveBeenCalled();
  });

  it('closes on a press outside the menu but not on one inside it', () => {
    const trigger = openMenu();

    option('en').dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
    flushSync();
    expect(expanded(trigger)).toBe('true');

    document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
    flushSync();
    expect(expanded(trigger)).toBe('false');

    trigger.click();
    flushSync();
    globalThis.dispatchEvent(new PointerEvent('pointerdown'));
    flushSync();
    expect(expanded(trigger)).toBe('false');

    globalThis.dispatchEvent(new PointerEvent('pointerdown'));
    flushSync();
    expect(expanded(trigger)).toBe('false');
  });

  it('closes on Escape and when focus leaves the menu', () => {
    const trigger = openMenu();

    press(option('en'), 'Escape');
    expect(expanded(trigger)).toBe('false');

    trigger.click();
    flushSync();
    const outside = document.createElement('button');
    document.body.append(outside);
    option('en').dispatchEvent(new FocusEvent('focusout', { bubbles: true, relatedTarget: outside }));
    flushSync();
    expect(expanded(trigger)).toBe('false');
  });
});
