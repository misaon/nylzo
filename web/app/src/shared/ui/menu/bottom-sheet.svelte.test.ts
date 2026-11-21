import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { BottomSheet } from './bottom-sheet.ts';

const HEIGHT = 260;

function sheetElement(): HTMLElement {
  const element = document.createElement('div');
  let shown = false;
  Object.defineProperties(element, {
    offsetHeight: { value: HEIGHT },
    showPopover: { value: () => (shown = true) },
    hidePopover: { value: () => (shown = false) },
    setPointerCapture: { value: vi.fn<(pointerId: number) => void>() },
    matches: { value: (selector: string) => selector === ':popover-open' && shown },
  });
  document.body.append(element);
  return element;
}

function pointer(
  type: string,
  { clientY, timeStamp }: { readonly clientY: number; readonly timeStamp: number },
): PointerEvent {
  const event = new PointerEvent(type, {
    pointerId: 1,
    isPrimary: true,
    button: 0,
    clientX: 200,
    clientY,
    bubbles: true,
  });
  Object.defineProperty(event, 'timeStamp', { value: timeStamp });
  return event;
}

describe('bottom sheet', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    document.body.replaceChildren();
  });

  it('closes on a throw that arrives as a single move', () => {
    const ondismiss = vi.fn<() => void>();
    const element = sheetElement();
    const sheet = new BottomSheet(element, ondismiss);
    sheet.setOpen(true);
    vi.advanceTimersByTime(2000);

    element.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    element.dispatchEvent(pointer('pointermove', { clientY: 670, timeStamp: 1060 }));
    element.dispatchEvent(pointer('pointerup', { clientY: 670, timeStamp: 1060 }));

    expect(ondismiss).toHaveBeenCalledOnce();
    sheet.destroy();
  });

  it('returns after a slow short drag', () => {
    const ondismiss = vi.fn<() => void>();
    const element = sheetElement();
    const sheet = new BottomSheet(element, ondismiss);
    sheet.setOpen(true);
    vi.advanceTimersByTime(2000);

    element.dispatchEvent(pointer('pointerdown', { clientY: 600, timeStamp: 1000 }));
    for (let step = 1; step <= 6; step++) {
      element.dispatchEvent(pointer('pointermove', { clientY: 600 + step * 10, timeStamp: 1000 + step * 200 }));
    }
    element.dispatchEvent(pointer('pointerup', { clientY: 660, timeStamp: 2200 }));
    vi.advanceTimersByTime(2000);

    expect(ondismiss).not.toHaveBeenCalled();
    expect(element.style.translate).toBe('0 0px');
    sheet.destroy();
  });
});
