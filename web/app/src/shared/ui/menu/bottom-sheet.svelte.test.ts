import { DetachedWindowAPI } from 'happy-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { BottomSheet } from './bottom-sheet.ts';

const HEIGHT = 260;
const SETTLE_MS = 2000;

interface TestSheet {
  readonly element: HTMLElement;
  readonly showPopover: ReturnType<typeof vi.fn<() => void>>;
  readonly hidePopover: ReturnType<typeof vi.fn<() => void>>;
  readonly ondismiss: ReturnType<typeof vi.fn<() => void>>;
  readonly sheet: BottomSheet;
}

function happyDom(): DetachedWindowAPI {
  const environment: object = globalThis;
  if (!('happyDOM' in environment) || !(environment.happyDOM instanceof DetachedWindowAPI)) {
    throw new TypeError('these tests need the happy-dom environment');
  }
  return environment.happyDOM;
}

function reducedMotion(matches: boolean): void {
  happyDom().settings.device.prefersReducedMotion = matches ? 'reduce' : 'no-preference';
}

function sheetElement({ height = HEIGHT }: { readonly height?: number } = {}): Omit<TestSheet, 'ondismiss' | 'sheet'> {
  const element = document.createElement('div');
  let shown = false;
  const showPopover = vi.fn<() => void>(() => {
    shown = true;
  });
  const hidePopover = vi.fn<() => void>(() => {
    shown = false;
  });
  Object.defineProperties(element, {
    offsetHeight: { value: height },
    showPopover: { value: showPopover },
    hidePopover: { value: hidePopover },
    setPointerCapture: { value: vi.fn<(pointerId: number) => void>() },
    matches: { value: (selector: string) => selector === ':popover-open' && shown },
    getBoundingClientRect: { value: () => new DOMRect(0, 500, 400, height) },
  });
  document.body.append(element);
  return { element, showPopover, hidePopover };
}

function openSheet(options: { readonly height?: number } = {}): TestSheet {
  const parts = sheetElement(options);
  const ondismiss = vi.fn<() => void>();
  const sheet = new BottomSheet(parts.element, ondismiss);
  sheet.setOpen(true);
  vi.advanceTimersByTime(SETTLE_MS);
  return { ...parts, ondismiss, sheet };
}

function pointer(
  type: string,
  {
    clientY,
    timeStamp,
    clientX = 200,
    pointerId = 1,
    isPrimary = true,
    button = 0,
  }: {
    readonly clientY: number;
    readonly timeStamp: number;
    readonly clientX?: number;
    readonly pointerId?: number;
    readonly isPrimary?: boolean;
    readonly button?: number;
  },
): PointerEvent {
  const event = new PointerEvent(type, { pointerId, isPrimary, button, clientX, clientY, bubbles: true });
  Object.defineProperty(event, 'timeStamp', { value: timeStamp });
  return event;
}

function offset(element: HTMLElement): number {
  return Number(element.style.translate.split(' ')[1]?.replace('px', ''));
}

describe('bottom sheet', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    reducedMotion(false);
    document.body.replaceChildren();
  });

  it('closes on a throw that arrives as a single move', () => {
    const { element, ondismiss, hidePopover, sheet } = openSheet();

    element.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    element.dispatchEvent(pointer('pointermove', { clientY: 670, timeStamp: 1060 }));
    element.dispatchEvent(pointer('pointerup', { clientY: 670, timeStamp: 1060 }));

    expect(ondismiss).toHaveBeenCalledOnce();
    vi.advanceTimersByTime(SETTLE_MS);
    expect(hidePopover).toHaveBeenCalledOnce();
    expect(element.style.translate).toBe(`0 ${HEIGHT}px`);
    sheet.destroy();
  });

  it('leaves a thrown sheet alone when the page already hid it', () => {
    const { element, ondismiss, hidePopover, sheet } = openSheet();

    element.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    element.dispatchEvent(pointer('pointermove', { clientY: 670, timeStamp: 1060 }));
    element.dispatchEvent(pointer('pointerup', { clientY: 670, timeStamp: 1060 }));
    element.hidePopover();
    vi.advanceTimersByTime(SETTLE_MS);

    expect(ondismiss).toHaveBeenCalledOnce();
    expect(hidePopover).toHaveBeenCalledOnce();
    sheet.destroy();
  });

  it('returns after a slow short drag', () => {
    const { element, ondismiss, sheet } = openSheet();

    element.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    for (let step = 1; step <= 6; step++) {
      element.dispatchEvent(pointer('pointermove', { clientY: 600 + step * 10, timeStamp: 1000 + step * 200 }));
    }
    element.dispatchEvent(pointer('pointerup', { clientY: 660, timeStamp: 2200 }));
    vi.advanceTimersByTime(SETTLE_MS);

    expect(ondismiss).not.toHaveBeenCalled();
    expect(element.style.translate).toBe('0 0px');
    sheet.destroy();
  });

  it('slides up when opened and hides the popover once it has slid down', () => {
    const { element, showPopover, hidePopover } = sheetElement();
    const sheet = new BottomSheet(element, vi.fn<() => void>());

    sheet.setOpen(true);
    sheet.setOpen(true);
    expect(showPopover).toHaveBeenCalledOnce();
    expect(offset(element)).toBeCloseTo(HEIGHT);
    expect(element.style.getPropertyValue('--sheet-shown')).toBe('0.000');

    vi.advanceTimersByTime(SETTLE_MS);
    expect(element.style.translate).toBe('0 0px');
    expect(element.style.getPropertyValue('--sheet-shown')).toBe('1.000');

    sheet.setOpen(false);
    expect(hidePopover).not.toHaveBeenCalled();
    vi.advanceTimersByTime(SETTLE_MS);
    expect(hidePopover).toHaveBeenCalledOnce();
    expect(element.style.translate).toBe(`0 ${HEIGHT}px`);
    sheet.destroy();
  });

  it('turns back from where it is when reopened while sliding down', () => {
    const { element, showPopover, hidePopover, sheet } = openSheet();

    sheet.setOpen(false);
    vi.advanceTimersByTime(50);
    const turnedAt = offset(element);
    sheet.setOpen(true);

    expect(turnedAt).toBeGreaterThan(0);
    expect(offset(element)).toBe(turnedAt);
    vi.advanceTimersByTime(SETTLE_MS);
    expect(element.style.translate).toBe('0 0px');
    expect(showPopover).toHaveBeenCalledOnce();
    expect(hidePopover).not.toHaveBeenCalled();
    sheet.destroy();
  });

  it('leaves a closing sheet alone when the page already hid it', () => {
    const { element, hidePopover, sheet } = openSheet();

    sheet.setOpen(false);
    element.hidePopover();
    vi.advanceTimersByTime(SETTLE_MS);

    expect(hidePopover).toHaveBeenCalledOnce();
    sheet.destroy();
  });

  it('jumps open and shut without sliding for reduced motion', () => {
    reducedMotion(true);
    const { element, hidePopover } = sheetElement();
    const sheet = new BottomSheet(element, vi.fn<() => void>());

    sheet.setOpen(true);
    expect(element.style.translate).toBe('0 0px');
    sheet.setOpen(false);
    expect(element.style.translate).toBe(`0 ${HEIGHT}px`);
    expect(hidePopover).toHaveBeenCalledOnce();
    sheet.destroy();
  });

  it('counts a sheet without height as hidden', () => {
    const { element, sheet } = openSheet({ height: 0 });

    expect(element.style.getPropertyValue('--sheet-shown')).toBe('0.000');
    sheet.destroy();
  });

  it('stays above the on-screen keyboard until destroyed', () => {
    const viewport = Object.assign(new EventTarget(), { height: 400, offsetTop: 0 });
    vi.stubGlobal('visualViewport', viewport);
    const { element, sheet } = openSheet();
    expect(element.style.bottom).toBe(`${innerHeight - 400}px`);

    viewport.height = 300;
    viewport.dispatchEvent(new Event('resize'));
    expect(element.style.bottom).toBe(`${innerHeight - 300}px`);
    viewport.offsetTop = 50;
    viewport.dispatchEvent(new Event('scroll'));
    expect(element.style.bottom).toBe(`${innerHeight - 350}px`);

    sheet.destroy();
    viewport.dispatchEvent(new Event('resize'));
    expect(element.style.bottom).toBe('');
  });

  it('drags only with the primary button on the sheet itself', () => {
    const { element, ondismiss, sheet } = openSheet();
    const field = document.createElement('textarea');
    element.append(field);
    const presses = [
      { target: element, init: { isPrimary: false } },
      { target: element, init: { button: 2 } },
      { target: field, init: {} },
    ];

    for (const { target, init } of presses) {
      target.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000, ...init }));
      target.dispatchEvent(pointer('pointermove', { clientY: 800, timeStamp: 1050, ...init }));
      target.dispatchEvent(pointer('pointerup', { clientY: 800, timeStamp: 1050, ...init }));
    }

    expect(element.style.translate).toBe('0 0px');
    expect(ondismiss).not.toHaveBeenCalled();
    sheet.destroy();
  });

  it('does not drag while closed', () => {
    const { element } = sheetElement();
    const ondismiss = vi.fn<() => void>();
    const sheet = new BottomSheet(element, ondismiss);

    element.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    element.dispatchEvent(pointer('pointermove', { clientY: 800, timeStamp: 1050 }));
    element.dispatchEvent(pointer('pointerup', { clientY: 800, timeStamp: 1050 }));

    expect(element.style.translate).toBe('');
    expect(ondismiss).not.toHaveBeenCalled();
    sheet.destroy();
  });

  it('lets a list that scrolls inside the sheet keep the gesture', () => {
    const { element, ondismiss, sheet } = openSheet();
    const list = document.createElement('ul');
    list.style.overflowY = 'auto';
    Object.defineProperties(list, { scrollHeight: { value: 900 }, clientHeight: { value: 300 } });
    const option = document.createElement('li');
    list.append(option);
    element.append(list);

    option.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    option.dispatchEvent(pointer('pointermove', { clientY: 800, timeStamp: 1050 }));
    option.dispatchEvent(pointer('pointerup', { clientY: 800, timeStamp: 1050 }));

    expect(element.style.translate).toBe('0 0px');
    expect(ondismiss).not.toHaveBeenCalled();
    sheet.destroy();
  });

  it('drags by content that overflows without scrolling and by text', () => {
    const { element, sheet } = openSheet();
    const overflowing = document.createElement('div');
    overflowing.style.overflowY = 'visible';
    Object.defineProperties(overflowing, { scrollHeight: { value: 900 }, clientHeight: { value: 300 } });
    const text = document.createTextNode('label');
    overflowing.append(text);
    element.append(overflowing);

    overflowing.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    overflowing.dispatchEvent(pointer('pointermove', { clientY: 640, timeStamp: 1300 }));
    expect(offset(element)).toBeCloseTo(30);
    overflowing.dispatchEvent(pointer('pointerup', { clientY: 640, timeStamp: 1300 }));
    vi.advanceTimersByTime(SETTLE_MS);

    text.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 3000 }));
    text.dispatchEvent(pointer('pointermove', { clientY: 650, timeStamp: 3050 }));
    expect(offset(element)).toBeCloseTo(40);
    sheet.destroy();
  });

  it('ignores sideways and tiny movements and other pointers', () => {
    const { element, ondismiss, sheet } = openSheet();

    element.dispatchEvent(pointer('pointermove', { clientY: 800, timeStamp: 900 }));
    element.dispatchEvent(pointer('pointerup', { clientY: 800, timeStamp: 900 }));
    element.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    element.dispatchEvent(pointer('pointermove', { clientY: 605, timeStamp: 1020 }));
    element.dispatchEvent(pointer('pointermove', { clientY: 640, clientX: 300, timeStamp: 1040 }));
    element.dispatchEvent(pointer('pointermove', { clientY: 800, timeStamp: 1060, pointerId: 2 }));
    element.dispatchEvent(pointer('pointerup', { clientY: 800, timeStamp: 1060, pointerId: 2 }));
    element.dispatchEvent(pointer('pointerup', { clientY: 640, timeStamp: 1080 }));

    expect(element.style.translate).toBe('0 0px');
    expect(ondismiss).not.toHaveBeenCalled();
    const click = new MouseEvent('click', { bubbles: true, cancelable: true, clientY: 600 });
    element.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(false);
    sheet.destroy();
  });

  it('resists a drag above its open position and springs back', () => {
    const { element, ondismiss, sheet } = openSheet();

    element.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    element.dispatchEvent(pointer('pointermove', { clientY: 500, timeStamp: 1050 }));
    const stretched = offset(element);
    element.dispatchEvent(pointer('pointerup', { clientY: 500, timeStamp: 1050 }));
    vi.advanceTimersByTime(SETTLE_MS);

    expect(stretched).toBeLessThan(0);
    expect(stretched).toBeGreaterThan(-90);
    expect(element.style.translate).toBe('0 0px');
    expect(ondismiss).not.toHaveBeenCalled();
    sheet.destroy();
  });

  it('does not throw a drag that moved in the same instant as the press', () => {
    const { element, ondismiss, sheet } = openSheet();

    element.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    element.dispatchEvent(pointer('pointermove', { clientY: 630, timeStamp: 1000 }));
    element.dispatchEvent(pointer('pointerup', { clientY: 630, timeStamp: 1000 }));
    vi.advanceTimersByTime(SETTLE_MS);

    expect(ondismiss).not.toHaveBeenCalled();
    expect(element.style.translate).toBe('0 0px');
    sheet.destroy();
  });

  it('springs back instead of closing when the pointer is cancelled', () => {
    const { element, ondismiss, sheet } = openSheet();

    element.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    element.dispatchEvent(pointer('pointermove', { clientY: 800, timeStamp: 1050 }));
    element.dispatchEvent(pointer('pointercancel', { clientY: 800, timeStamp: 1050 }));
    vi.advanceTimersByTime(SETTLE_MS);

    expect(ondismiss).not.toHaveBeenCalled();
    expect(element.style.translate).toBe('0 0px');
    sheet.destroy();
  });

  it('swallows the click that ends a drag', () => {
    const { element, sheet } = openSheet();
    const button = document.createElement('button');
    element.append(button);
    const clicked = vi.fn<() => void>();
    button.addEventListener('click', clicked);

    button.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    button.dispatchEvent(pointer('pointermove', { clientY: 640, timeStamp: 1050 }));
    button.dispatchEvent(pointer('pointerup', { clientY: 640, timeStamp: 1050 }));
    const afterDrag = new MouseEvent('click', { bubbles: true, cancelable: true });
    button.dispatchEvent(afterDrag);
    button.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));

    expect(afterDrag.defaultPrevented).toBe(true);
    expect(clicked).toHaveBeenCalledOnce();
    sheet.destroy();
  });

  it('dismisses on a click on the backdrop around the sheet', () => {
    const { element, ondismiss, sheet } = openSheet();
    const clicks = [
      { clientX: 200, clientY: 100 },
      { clientX: 200, clientY: 900 },
      { clientX: -10, clientY: 600 },
      { clientX: 500, clientY: 600 },
    ];

    element.dispatchEvent(new MouseEvent('click', { bubbles: true, clientX: 200, clientY: 600 }));
    expect(ondismiss).not.toHaveBeenCalled();
    for (const position of clicks) {
      element.dispatchEvent(new MouseEvent('click', { bubbles: true, ...position }));
    }

    expect(ondismiss).toHaveBeenCalledTimes(clicks.length);
    sheet.destroy();
  });

  it('cleans up a sheet that was never opened', () => {
    const { element, hidePopover } = sheetElement();
    const sheet = new BottomSheet(element, vi.fn<() => void>());

    sheet.destroy();

    expect(hidePopover).not.toHaveBeenCalled();
    expect(element.getAttribute('style')).toBeFalsy();
  });
});
