import { hydrate, mount } from 'svelte';

import '../app.css';
import { prepareWorker } from '../shared/kdf/worker-source.ts';
import { browserPreferences, type Language, savedLanguage, storedLanguage } from '../shared/language/language.ts';
import { takeHandoff } from '../shared/local-client/handoff.ts';
import { HOME_PATHS, homeLanguage, screenOf } from './pages.ts';

const FONT_WAIT_MS = 1500;
const PAINT_WAIT_MS = 1000;
const FONTS = ['300 1em Sora', '400 1em Geist', '500 1em Geist', '400 1em "Geist Mono"'];
const HOME_FONT_SAMPLES: Readonly<Record<Language, string>> = { en: 'Aa', cs: 'Aáčř' };
const ANY_TEXT_SAMPLE = 'Aáčřł';

async function loadFonts(sample: string): Promise<void> {
  try {
    await Promise.all(FONTS.map(async (font) => document.fonts.load(font, sample)));
  } catch {
    return;
  }
}

async function fontsLoaded(sample: string): Promise<void> {
  const timeout = new Promise<void>((resolve) => setTimeout(resolve, FONT_WAIT_MS));
  await Promise.race([loadFonts(sample), timeout]);
}

async function nextTask(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function contentPainted(): Promise<void> {
  return new Promise((resolve) => {
    new PerformanceObserver((entries, observer) => {
      if (entries.getEntriesByName('first-contentful-paint').length === 0) {
        return;
      }
      observer.disconnect();
      resolve();
    }).observe({ type: 'paint', buffered: true });
    setTimeout(resolve, PAINT_WAIT_MS);
  });
}

async function boot(): Promise<void> {
  await takeHandoff();
  const path = location.pathname;
  const screen = screenOf(path);
  if (screen === 'create' || screen === 'read') {
    void prepareWorker();
  }
  const preferences = browserPreferences();
  const homePage = homeLanguage(path);
  const savedChoice = storedLanguage(preferences);
  if (homePage && savedChoice && savedChoice !== homePage) {
    location.replace(`${HOME_PATHS[savedChoice]}${location.search}${location.hash}`);
    return;
  }
  const language = homePage ?? savedLanguage(preferences, navigator.languages);
  document.documentElement.lang = language;
  await fontsLoaded(homePage ? HOME_FONT_SAMPLES[homePage] : ANY_TEXT_SAMPLE);
  document.documentElement.dataset['fonts'] = '';
  if (document.body.firstElementChild) {
    await contentPainted();
  }
  const { App, prepare, tidy } = await import('./start.ts');
  await nextTask();
  prepare();
  const options = { target: document.body, props: { start: { path, language } } };
  if (document.body.firstElementChild) {
    hydrate(App, options);
  } else {
    mount(App, options);
  }
  document.documentElement.dataset['ready'] = '';
  void tidy();
}

void boot();
