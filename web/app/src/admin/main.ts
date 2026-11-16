import { mount } from 'svelte';

import './admin.css';
import Admin from './Admin.svelte';
import { adminLanguage } from './messages/catalog.ts';

const FONT_WAIT_MS = 1500;
const FONTS = ['300 1em Sora', '400 1em Geist', '500 1em Geist'];
const FONT_SAMPLE = 'Aáčřł';

async function fontsLoaded(): Promise<void> {
  try {
    await Promise.all(FONTS.map(async (font) => document.fonts.load(font, FONT_SAMPLE)));
  } catch {}
}

async function loadFonts(): Promise<void> {
  const loaded = fontsLoaded();
  const waited = new Promise((resolve) => {
    setTimeout(resolve, FONT_WAIT_MS);
  });
  await Promise.race([loaded, waited]);
}

async function boot(): Promise<void> {
  const language = adminLanguage();
  document.documentElement.lang = language;
  await loadFonts();
  document.documentElement.dataset['fonts'] = '';
  mount(Admin, { target: document.body, props: { language } });
  document.documentElement.dataset['ready'] = '';
}

// oxlint-disable-next-line unicorn/prefer-top-level-await -- WebKit font readiness must not wait for this module to finish evaluating.
void boot();
