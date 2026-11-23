import { afterEach, describe, expect, it, vi } from 'vitest';

import { submitOnEnter } from './private-fields.ts';

function formWithField(): { readonly field: HTMLInputElement; readonly submitted: ReturnType<typeof vi.fn> } {
  const form = document.createElement('form');
  const field = document.createElement('input');
  form.append(field);
  document.body.append(form);
  const submitted = vi.fn<(event: SubmitEvent) => void>((event) => {
    event.preventDefault();
  });
  form.addEventListener('submit', submitted);
  field.addEventListener('keydown', submitOnEnter);
  return { field, submitted };
}

describe('private fields', () => {
  afterEach(() => {
    document.body.replaceChildren();
  });

  it('submits the form on Enter', () => {
    const { field, submitted } = formWithField();
    const enter = new KeyboardEvent('keydown', { key: 'Enter', cancelable: true });

    field.dispatchEvent(enter);

    expect(submitted).toHaveBeenCalledOnce();
    expect(enter.defaultPrevented).toBe(true);
  });

  it('does not submit on other keys or on an Enter that confirms an IME conversion', () => {
    const { field, submitted } = formWithField();

    field.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }));
    field.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true }));
    // oxlint-disable-next-line typescript/no-deprecated -- Safari marks the Enter that confirms an IME conversion only with keyCode 229.
    field.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', keyCode: 229 }));

    expect(submitted).not.toHaveBeenCalled();
  });

  it('ignores an Enter that is not being dispatched to an element', () => {
    const enter = new KeyboardEvent('keydown', { key: 'Enter', cancelable: true });

    submitOnEnter(enter);

    expect(enter.defaultPrevented).toBe(false);
  });
});
