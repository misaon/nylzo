import { hydrate, mount } from 'svelte';

import App from './App.svelte';
import './app.css';
import { browserPreferences, savedLanguage, storedLanguage } from './lib/i18n.svelte.ts';
import { HOME_PATHS, homeLanguage } from './lib/pages.ts';
import { purgeSentRecords } from './lib/sent.ts';

const FONT_WAIT_MS = 1500;
const FONTS = ['300 1em Sora', '400 1em Geist', '500 1em Geist', '400 1em "Geist Mono"'];

async function fontsLoaded(): Promise<void> {
  if (!('fonts' in document)) {
    return;
  }
  const loads = Promise.all(FONTS.map((font) => document.fonts.load(font, 'Aáčř'))).then(() => undefined);
  const timeout = new Promise<void>((resolve) => setTimeout(resolve, FONT_WAIT_MS));
  await Promise.race([loads.catch(() => undefined), timeout]);
}

const path = location.pathname;
const preferences = browserPreferences();
const home = homeLanguage(path);
const chosen = storedLanguage(preferences);

if (home && chosen && chosen !== home) {
  location.replace(`${HOME_PATHS[chosen]}${location.search}${location.hash}`);
} else {
  const language = home ?? savedLanguage(preferences, navigator.languages);
  document.documentElement.lang = language;
  await fontsLoaded();
  const options = { target: document.body, props: { start: { path, language } } };
  if (document.body.firstElementChild) {
    hydrate(App, options);
  } else {
    mount(App, options);
  }
  document.documentElement.dataset['ready'] = '';
  purgeSentRecords(globalThis.indexedDB, Date.now()).catch(() => undefined);
}
