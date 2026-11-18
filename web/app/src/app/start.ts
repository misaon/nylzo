import { hydrate, mount } from 'svelte';

import type { Language } from '../shared/language/language.ts';
import { servedByLocalClient } from '../shared/local-client/instance-origin.ts';
import { browserDatabases } from '../shared/sent-records/indexed-database.ts';
import { keepPurgingSentRecords } from '../shared/sent-records/sent-store.ts';
import App from './App.svelte';
import { forgetRetiredRecovery } from './retired-recovery.ts';

export function show(path: string, language: Language): void {
  const options = { target: document.body, props: { start: { path, language } } };
  const skeleton = document.body.querySelector(':scope > [data-skeleton]');
  if (skeleton) {
    document.documentElement.dataset['skeleton'] = '';
    mount(App, options);
    skeleton.remove();
  } else if (document.body.firstElementChild) {
    hydrate(App, options);
  } else {
    mount(App, options);
  }
}

export function prepare(): void {
  if (!servedByLocalClient()) {
    forgetRetiredRecovery(() => sessionStorage, browserDatabases());
  }
}

export async function tidy(): Promise<void> {
  if (!servedByLocalClient()) {
    await keepPurgingSentRecords(browserDatabases(), () => Date.now());
  }
}
