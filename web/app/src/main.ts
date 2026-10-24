import { mount } from 'svelte';

import App from './App.svelte';
import './app.css';
import { browserPreferences, savedLanguage } from './lib/i18n.svelte.ts';
import { purgeSentRecords } from './lib/sent.ts';

const FONT_WAIT_MS = 1500;
const FONTS = ['300 1em Sora', '400 1em Geist', '500 1em Geist'];

async function fontsLoaded(): Promise<void> {
  const loads = Promise.all(FONTS.map((font) => document.fonts.load(font, 'Aáčř'))).then(() => undefined);
  const timeout = new Promise<void>((resolve) => setTimeout(resolve, FONT_WAIT_MS));
  await Promise.race([loads.catch(() => undefined), timeout]);
}

document.documentElement.lang = savedLanguage(browserPreferences(), navigator.languages);

await fontsLoaded();
mount(App, { target: document.body });

purgeSentRecords(globalThis.indexedDB, Date.now()).catch(() => undefined);
