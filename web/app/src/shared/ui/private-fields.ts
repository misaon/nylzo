import type { HTMLInputAttributes } from 'svelte/elements';

export const PRIVATE_TEXT = {
  autocomplete: 'off',
  autocapitalize: 'off',
  autocorrect: 'off',
  spellcheck: 'false',
  translate: 'no',
  writingsuggestions: 'false',
} as const satisfies HTMLInputAttributes;

// No element has this id, so the field has no form owner and a form submission never carries it to a password manager.
const NO_FORM = 'nylzo-no-form';

export const PRIVATE_PASSPHRASE = {
  ...PRIVATE_TEXT,
  autocomplete: 'one-time-code',
  form: NO_FORM,
  'data-1p-ignore': '',
  'data-lpignore': 'true',
  'data-bwignore': 'true',
  'data-form-type': 'other',
  'data-protonpass-ignore': 'true',
} as const satisfies HTMLInputAttributes;

export function submitOnEnter(event: KeyboardEvent): void {
  if (event.key !== 'Enter' || event.isComposing || !(event.currentTarget instanceof HTMLElement)) {
    return;
  }
  event.preventDefault();
  event.currentTarget.closest('form')?.requestSubmit();
}
