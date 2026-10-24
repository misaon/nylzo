import { hydrate, mount } from 'svelte';

import App from './App.svelte';
import type { Language } from './lib/language.ts';
import { purgeSentRecords } from './lib/sent.ts';

export function start(page: { readonly path: string; readonly language: Language }): void {
  const options = { target: document.body, props: { start: page } };
  if (document.body.firstElementChild) {
    hydrate(App, options);
  } else {
    mount(App, options);
  }
  document.documentElement.dataset['ready'] = '';
  purgeSentRecords(globalThis.indexedDB, Date.now()).catch(() => undefined);
}
