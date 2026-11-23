import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import Tooltip from './Tooltip.svelte';

type TipState = 'on hover' | 'pinned' | 'dismissed';

function escape(key = 'Escape'): void {
  document.dispatchEvent(new KeyboardEvent('keydown', { key }));
  flushSync();
}

describe('tooltip', () => {
  let component: ReturnType<typeof mount>;
  let wrapper: HTMLElement;
  let button: HTMLButtonElement;
  let tip: HTMLElement;

  beforeEach(() => {
    const target = document.createElement('div');
    document.body.append(target);
    component = mount(Tooltip, { target, props: { label: 'Label', title: 'Title', text: 'Text' } });
    flushSync();
    const foundWrapper = target.firstElementChild;
    const foundButton = target.querySelector('button');
    const foundTip = target.querySelector('[role="tooltip"]');
    if (!(foundWrapper instanceof HTMLElement) || !foundButton || !(foundTip instanceof HTMLElement)) {
      throw new TypeError('the tooltip did not render');
    }
    wrapper = foundWrapper;
    button = foundButton;
    tip = foundTip;
  });

  afterEach(async () => {
    await unmount(component);
    document.body.replaceChildren();
  });

  function state(): TipState {
    if (tip.classList.contains('group-hover/tip:opacity-100')) {
      return 'on hover';
    }
    return tip.classList.contains('opacity-100') ? 'pinned' : 'dismissed';
  }

  it('pins on click, unpins on a second click and on blur', () => {
    expect(state()).toBe('on hover');

    button.click();
    flushSync();
    expect(state()).toBe('pinned');
    button.click();
    flushSync();
    expect(state()).toBe('on hover');

    button.click();
    button.dispatchEvent(new FocusEvent('blur'));
    flushSync();
    expect(state()).toBe('on hover');
  });

  it('is dismissed by Escape while pinned until the pointer or focus leaves', () => {
    button.click();
    flushSync();
    escape('Enter');
    expect(state()).toBe('pinned');

    escape();
    expect(state()).toBe('dismissed');
    wrapper.dispatchEvent(new PointerEvent('pointerleave'));
    flushSync();
    expect(state()).toBe('on hover');

    button.click();
    flushSync();
    escape();
    expect(state()).toBe('dismissed');
    button.dispatchEvent(new FocusEvent('focusout', { bubbles: true }));
    flushSync();
    expect(state()).toBe('on hover');
  });

  it('ignores Escape when it is neither pinned, hovered nor focused', () => {
    escape();

    expect(state()).toBe('on hover');
  });
});
