import { hydrate, mount } from 'svelte';

import App from './App.svelte';
import type { Language } from '../shared/language/language.ts';
import { purgeSentRecords } from '../shared/sent-records/sent-records.ts';

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
