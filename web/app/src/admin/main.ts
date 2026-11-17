import { mount } from 'svelte';

import './admin.css';
import Admin from './Admin.svelte';
import { adminLanguage } from './messages/catalog.ts';
import type { AdminLanguage } from './messages/catalog.ts';

const FONT_WAIT_MS = 1500;
const FONTS = ['300 1em Sora', '400 1em Geist', '500 1em Geist'];
const FONT_SAMPLES: Readonly<Record<AdminLanguage, string>> = { en: 'Aa', cs: 'Aáčř' };

async function fontsLoaded(sample: string): Promise<void> {
  try {
    await Promise.all(FONTS.map(async (font) => document.fonts.load(font, sample)));
  } catch {}
}

async function loadFonts(sample: string): Promise<void> {
  const loaded = fontsLoaded(sample);
  const waited = new Promise((resolve) => {
    setTimeout(resolve, FONT_WAIT_MS);
  });
  await Promise.race([loaded, waited]);
}

async function boot(): Promise<void> {
  const language = adminLanguage();
  document.documentElement.lang = language;
  await loadFonts(FONT_SAMPLES[language]);
  document.documentElement.dataset['fonts'] = '';
  mount(Admin, { target: document.body, props: { language } });
  document.documentElement.dataset['ready'] = '';
}

// oxlint-disable-next-line unicorn/prefer-top-level-await -- WebKit font readiness must not wait for this module to finish evaluating.
void boot();
