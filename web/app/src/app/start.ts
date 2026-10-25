import { hydrate, mount } from 'svelte';

import type { Language } from '../shared/language/language.ts';

import { browserDatabases, purgeSentRecords } from '../shared/sent-records/sent-store.ts';
import App from './App.svelte';

export function start(page: { readonly path: string; readonly language: Language }): void {
  const options = { target: document.body, props: { start: page } };
  if (document.body.firstElementChild) {
    hydrate(App, options);
  } else {
    mount(App, options);
  }
  document.documentElement.dataset['ready'] = '';
  void purgeSentRecords(browserDatabases(), Date.now());
}
