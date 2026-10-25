import '../app.css';
import { browserPreferences, savedLanguage, storedLanguage, type Language } from '../shared/i18n/language.ts';
import { HOME_PATHS, homeLanguage } from './pages.ts';

const FONT_WAIT_MS = 1500;
const PAINT_WAIT_MS = 1000;
const FONTS = ['300 1em Sora', '400 1em Geist', '500 1em Geist', '400 1em "Geist Mono"'];
const HOME_FONT_SAMPLES: Readonly<Record<Language, string>> = { en: 'Aa', cs: 'Aáčř' };
const ANY_TEXT_SAMPLE = 'Aáčřł';

async function fontsLoaded(sample: string): Promise<void> {
  if (!('fonts' in document)) {
    return;
  }
  const loads = Promise.all(FONTS.map((font) => document.fonts.load(font, sample))).then(() => undefined);
  const timeout = new Promise<void>((resolve) => setTimeout(resolve, FONT_WAIT_MS));
  await Promise.race([loads.catch(() => undefined), timeout]);
}

function nextTask(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve));
}

function contentPainted(): Promise<void> {
  return new Promise((resolve) => {
    const observer = PerformanceObserver.supportedEntryTypes.includes('paint')
      ? new PerformanceObserver((entries) => {
          if (entries.getEntriesByName('first-contentful-paint').length > 0) {
            observer?.disconnect();
            resolve();
          }
        })
      : undefined;
    observer?.observe({ type: 'paint', buffered: true });
    setTimeout(resolve, PAINT_WAIT_MS);
  });
}

async function boot(): Promise<void> {
  const path = location.pathname;
  const preferences = browserPreferences();
  const home = homeLanguage(path);
  const chosen = storedLanguage(preferences);
  if (home && chosen && chosen !== home) {
    location.replace(`${HOME_PATHS[chosen]}${location.search}${location.hash}`);
    return;
  }
  const language = home ?? savedLanguage(preferences, navigator.languages);
  document.documentElement.lang = language;
  await fontsLoaded(home ? HOME_FONT_SAMPLES[home] : ANY_TEXT_SAMPLE);
  document.documentElement.dataset['fonts'] = '';
  if (document.body.firstElementChild) {
    await contentPainted();
  }
  const { start } = await import('./start.ts');
  await nextTask();
  start({ path, language });
}

void boot();
