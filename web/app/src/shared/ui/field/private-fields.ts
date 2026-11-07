import type { HTMLInputAttributes } from 'svelte/elements';

export const PRIVATE_TEXT = {
  autocomplete: 'off',
  autocapitalize: 'off',
  autocorrect: 'off',
  spellcheck: 'false',
  translate: 'no',
  writingsuggestions: 'false',
  'data-gramm': 'false',
  'data-gramm_editor': 'false',
  'data-enable-grammarly': 'false',
  'data-lt-active': 'false',
} as const satisfies HTMLInputAttributes;

// No element has this id, so the field has no form owner and a form submission never carries it to a password manager.
const NO_FORM = 'nylzo-no-form';

const PRIVATE_PASSPHRASE = {
  ...PRIVATE_TEXT,
  form: NO_FORM,
  'data-1p-ignore': '',
  'data-lpignore': 'true',
  'data-bwignore': 'true',
  'data-form-type': 'other',
  'data-protonpass-ignore': 'true',
} as const satisfies HTMLInputAttributes;

// A hidden field must not look like a password to a password manager; a shown one must keep no form state for session restore.
export function privatePassphrase(shown: boolean): HTMLInputAttributes {
  return { ...PRIVATE_PASSPHRASE, autocomplete: shown ? 'off' : 'one-time-code' };
}

export function submitOnEnter(event: KeyboardEvent): void {
  if (event.key !== 'Enter' || event.isComposing || !(event.currentTarget instanceof HTMLElement)) {
    return;
  }
  event.preventDefault();
  event.currentTarget.closest('form')?.requestSubmit();
}
