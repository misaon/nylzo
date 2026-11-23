import { DetachedWindowAPI } from 'happy-dom';
import { createRawSnippet, flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import Popover from './Popover.svelte';

const WIDE = { width: 1024, height: 768 };
const NARROW = { width: 500, height: 800 };

function happyDom(): DetachedWindowAPI {
  const environment: object = globalThis;
  if (!('happyDOM' in environment) || !(environment.happyDOM instanceof DetachedWindowAPI)) {
    throw new TypeError('these tests need the happy-dom environment');
  }
  return environment.happyDOM;
}

function menuTarget({ menu = true }: { readonly menu?: boolean } = {}): HTMLElement {
  const target = document.createElement('div');
  target.toggleAttribute('data-menu', menu);
  target.append(document.createElement('button'));
  document.body.append(target);
  return target;
}

function leave(dialog: HTMLElement, relatedTarget: EventTarget | null): void {
  dialog
    .querySelector('button:not(:disabled)')
    ?.dispatchEvent(new FocusEvent('focusout', { bubbles: true, relatedTarget }));
}

function outsideButton(): HTMLButtonElement {
  const button = document.createElement('button');
  document.body.append(button);
  return button;
}

describe('popover', () => {
  const onclose = vi.fn<() => void>();
  const ondismiss = vi.fn<() => void>();
  const children = createRawSnippet(() => ({
    render: () => '<div><button disabled>first</button><button>second</button></div>',
  }));
  const props: { open: boolean; from: 'above' | 'below' } = $state({ open: false, from: 'above' });
  let component: ReturnType<typeof mount> | undefined;

  beforeEach(() => {
    vi.resetAllMocks();
    props.open = false;
    props.from = 'above';
  });

  afterEach(async () => {
    if (component) {
      await unmount(component);
      component = undefined;
    }
    happyDom().setViewport(WIDE);
    document.body.replaceChildren();
  });

  function show(target: Document | Element | ShadowRoot = menuTarget()): HTMLElement {
    component = mount(Popover, {
      target,
      props: {
        id: 'menu',
        label: 'Menu',
        get open() {
          return props.open;
        },
        get from() {
          return props.from;
        },
        onclose,
        ondismiss,
        children,
      },
    });
    flushSync();
    const dialog = target.querySelector('[role="dialog"]');
    if (!(dialog instanceof HTMLElement)) {
      throw new TypeError('the popover rendered no dialog');
    }
    return dialog;
  }

  describe('as a dropdown', () => {
    it('stays inert and hidden until opened, then focuses its first enabled control', () => {
      const dialog = show();
      expect(dialog.inert).toBe(true);
      expect(dialog.classList.contains('invisible')).toBe(true);
      expect(document.activeElement).toBe(document.body);

      props.open = true;
      flushSync();

      expect(dialog.inert).toBe(false);
      expect(dialog.classList.contains('visible')).toBe(true);
      expect(document.activeElement?.textContent).toBe('second');
      expect(dialog.querySelector('[aria-hidden="true"]')).toBeNull();
    });

    it('grows from the edge it opens from', () => {
      const dialog = show();
      expect(dialog.classList.contains('origin-top')).toBe(true);
      expect(dialog.classList.contains('-translate-y-1')).toBe(true);

      props.from = 'below';
      flushSync();
      expect(dialog.classList.contains('origin-bottom')).toBe(true);
      expect(dialog.classList.contains('translate-y-1')).toBe(true);
    });

    it('closes on Escape without letting the key reach the page', () => {
      props.open = true;
      const dialog = show();
      const pageKeys = vi.fn<() => void>();
      document.addEventListener('keydown', pageKeys);

      dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
      expect(onclose).not.toHaveBeenCalled();
      dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

      expect(onclose).toHaveBeenCalledOnce();
      expect(pageKeys).toHaveBeenCalledOnce();
      document.removeEventListener('keydown', pageKeys);
    });

    it('is dismissed when focus leaves the whole menu while open', () => {
      props.open = true;
      const target = menuTarget();
      const dialog = show(target);

      leave(dialog, target.querySelector('button'));
      leave(dialog, null);
      expect(ondismiss).not.toHaveBeenCalled();
      leave(dialog, outsideButton());
      expect(ondismiss).toHaveBeenCalledOnce();

      props.open = false;
      flushSync();
      leave(dialog, outsideButton());
      expect(ondismiss).toHaveBeenCalledOnce();
    });

    it('treats its parent as the menu when no menu is marked', () => {
      props.open = true;
      const target = menuTarget({ menu: false });
      const dialog = show(target);

      leave(dialog, target.querySelector('button'));
      expect(ondismiss).not.toHaveBeenCalled();
      leave(dialog, outsideButton());
      expect(ondismiss).toHaveBeenCalledOnce();
    });

    it('treats itself as the menu at the root of a shadow tree', () => {
      props.open = true;
      const host = document.createElement('div');
      document.body.append(host);
      const root = host.attachShadow({ mode: 'open' });
      const sibling = document.createElement('button');
      root.append(sibling);
      const dialog = show(root);

      leave(dialog, dialog.querySelector('button:not(:disabled)'));
      expect(ondismiss).not.toHaveBeenCalled();
      leave(dialog, sibling);
      expect(ondismiss).toHaveBeenCalledOnce();
    });
  });

  describe('as a bottom sheet', () => {
    const shownPopovers = new WeakSet<Element>();
    const showPopover = vi.fn<(element: Element) => void>();
    const hidePopover = vi.fn<(element: Element) => void>();

    beforeEach(() => {
      vi.useFakeTimers();
      happyDom().setViewport(NARROW);
      Object.defineProperties(HTMLElement.prototype, {
        showPopover: {
          configurable: true,
          value(this: HTMLElement) {
            shownPopovers.add(this);
            showPopover(this);
          },
        },
        hidePopover: {
          configurable: true,
          value(this: HTMLElement) {
            shownPopovers.delete(this);
            hidePopover(this);
          },
        },
      });
      // oxlint-disable-next-line typescript/unbound-method -- the spy calls the original with the element as this.
      const matches = Element.prototype.matches;
      vi.spyOn(Element.prototype, 'matches').mockImplementation(function popoverAware(this: Element, selectors) {
        return selectors === ':popover-open' ? shownPopovers.has(this) : matches.call(this, selectors);
      });
    });

    afterEach(() => {
      vi.useRealTimers();
      vi.restoreAllMocks();
      Reflect.deleteProperty(HTMLElement.prototype, 'showPopover');
      Reflect.deleteProperty(HTMLElement.prototype, 'hidePopover');
    });

    it('slides up as a popover with a grabber and slides away when closed', () => {
      const dialog = show();
      expect(dialog.getAttribute('popover')).toBe('manual');
      expect(dialog.firstElementChild?.getAttribute('aria-hidden')).toBe('true');
      expect(showPopover).not.toHaveBeenCalled();

      props.open = true;
      flushSync();
      expect(showPopover).toHaveBeenCalledExactlyOnceWith(dialog);
      expect(document.activeElement?.textContent).toBe('second');

      props.open = false;
      flushSync();
      vi.advanceTimersByTime(2000);
      expect(hidePopover).toHaveBeenCalledExactlyOnceWith(dialog);
    });

    it('is dismissed by a tap on the backdrop', () => {
      props.open = true;
      const dialog = show();

      dialog.dispatchEvent(new MouseEvent('click', { bubbles: true, clientX: 10, clientY: 10 }));

      expect(ondismiss).toHaveBeenCalledOnce();
    });

    it('turns between a dropdown and a sheet as the screen width changes', () => {
      happyDom().setViewport(WIDE);
      props.open = true;
      const target = menuTarget();
      show(target);
      expect(target.querySelector('[role="dialog"]')?.hasAttribute('popover')).toBe(false);

      happyDom().setViewport(NARROW);
      flushSync();
      const sheet = target.querySelector('[role="dialog"]');
      expect(sheet?.getAttribute('popover')).toBe('manual');
      expect(showPopover).toHaveBeenCalledExactlyOnceWith(sheet);

      happyDom().setViewport(WIDE);
      flushSync();
      expect(target.querySelector('[role="dialog"]')?.hasAttribute('popover')).toBe(false);
      expect(hidePopover).toHaveBeenCalledExactlyOnceWith(sheet);
    });
  });
});
