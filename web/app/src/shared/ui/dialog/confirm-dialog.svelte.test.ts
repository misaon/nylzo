import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import ConfirmDialog from './ConfirmDialog.svelte';

function buttons(dialog: HTMLDialogElement): { readonly dismiss: HTMLElement; readonly confirm: HTMLElement } {
  const [dismiss, confirm] = dialog.querySelectorAll('button');
  if (!dismiss || !confirm) {
    throw new TypeError('the dialog needs two buttons');
  }
  return { dismiss, confirm };
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

describe('confirm dialog', () => {
  const onconfirm = vi.fn<() => void>();
  const onclose = vi.fn<() => void>();
  let component: ReturnType<typeof mount> | undefined;
  let target: HTMLElement;

  beforeEach(() => {
    vi.resetAllMocks();
    target = document.createElement('div');
    document.body.append(target);
  });

  async function remove(): Promise<void> {
    if (component) {
      await unmount(component);
      component = undefined;
    }
  }

  afterEach(async () => {
    await remove();
    document.body.replaceChildren();
  });

  function show(props: { readonly message?: string; readonly tone?: 'danger' | 'warning' } = {}): HTMLDialogElement {
    component = mount(ConfirmDialog, {
      target,
      props: {
        id: 'confirm',
        title: 'Title',
        confirmLabel: 'Delete',
        dismissLabel: 'Keep',
        onconfirm,
        onclose,
        ...props,
      },
    });
    flushSync();
    const dialog = target.querySelector('dialog');
    if (!dialog) {
      throw new TypeError('the dialog did not render');
    }
    return dialog;
  }

  it('opens as a modal and confirms a dangerous action once', async () => {
    const dialog = show();
    const { confirm } = buttons(dialog);
    expect(dialog.open).toBe(true);
    expect(dialog.hasAttribute('aria-describedby')).toBe(false);
    expect(confirm.querySelector('svg')).not.toBeNull();

    confirm.click();
    confirm.click();
    await settle();

    expect(dialog.open).toBe(false);
    expect(onconfirm).toHaveBeenCalledOnce();
    expect(onclose).not.toHaveBeenCalled();
  });

  it('describes itself with its message and asks a warning without an icon', () => {
    const dialog = show({ message: 'Why', tone: 'warning' });
    const { confirm } = buttons(dialog);

    expect(dialog.getAttribute('aria-describedby')).toBe('confirm-message');
    expect(target.querySelector('#confirm-message')?.textContent).toBe('Why');
    expect(confirm.querySelector('svg')).toBeNull();
  });

  it('reports a dismissal from its button or from Escape', async () => {
    const { dismiss } = buttons(show());
    dismiss.click();
    await settle();
    expect(onclose).toHaveBeenCalledOnce();

    await remove();
    show().dispatchEvent(new Event('cancel'));
    await settle();
    expect(onclose).toHaveBeenCalledTimes(2);
    expect(onconfirm).not.toHaveBeenCalled();
  });

  it('reports only after its closing animation finishes', async () => {
    const dialog = show();
    const animation = new Animation();
    const finished = Promise.withResolvers<Animation>();
    Object.defineProperty(animation, 'finished', { value: finished.promise });
    vi.spyOn(dialog, 'getAnimations').mockReturnValue([animation]);

    buttons(dialog).confirm.click();
    await settle();
    expect(onconfirm).not.toHaveBeenCalled();

    finished.resolve(animation);
    await vi.waitFor(() => {
      expect(onconfirm).toHaveBeenCalledOnce();
    });
  });

  it('still reports a close that reaches it after it was removed', async () => {
    const dialog = show();
    await remove();

    dialog.dispatchEvent(new Event('close'));
    await settle();

    expect(onclose).toHaveBeenCalledOnce();
  });
});
