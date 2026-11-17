import '../app.css';
import { prepareWorker } from '../shared/kdf/worker-source.ts';
import { loadCatalog } from '../shared/language/catalog.ts';
import {
  browserPreferences,
  homePath,
  LANGUAGE_DETAILS,
  savedLanguage,
  storedLanguage,
} from '../shared/language/language.ts';
import type { Language } from '../shared/language/language.ts';
import { takeHandoff } from '../shared/local-client/handoff.ts';
import { homeLanguage, screenOf } from './pages.ts';
import type { Screen } from './pages.ts';

const FONT_WAIT_MS = 1500;
const PAINT_WAIT_MS = 1000;
const FONTS = ['300 1em Sora', '400 1em Geist', '500 1em Geist'];
const ANY_TEXT_SAMPLE = 'Aáčřł';

async function loadFonts(sample: string): Promise<void> {
  try {
    await Promise.all(FONTS.map(async (font) => document.fonts.load(font, sample)));
  } catch {}
}

async function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, milliseconds);
  });
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

async function loadApp(language: Language) {
  const [start] = await Promise.all([import('./start.ts'), loadCatalog(language)]);
  return start;
}

// Loaded only on a counted screen, so a page that shows a message holds no code that reports a visit.
async function watchVisit(screen: Exclude<Screen, 'read'>): Promise<void> {
  try {
    const { watchBrowserVisit } = await import('../shared/visits/visits.ts');
    watchBrowserVisit(screen);
  } catch {}
}

async function boot(): Promise<void> {
  await takeHandoff();
  const path = location.pathname;
  const screen = screenOf(path);
  if (screen === 'create' || screen === 'read') {
    void prepareWorker(screen === 'create' ? 'low' : 'auto');
  }
  const preferences = browserPreferences();
  const homePage = homeLanguage(path);
  const savedChoice = storedLanguage(preferences);
  if (homePage && savedChoice && savedChoice !== homePage) {
    location.replace(`${homePath(savedChoice)}${location.search}${location.hash}`);
    return;
  }
  const language = homePage ?? savedLanguage(preferences, navigator.languages);
  document.documentElement.lang = language;
  // A prerendered home page shows its content first, so its code waits for that paint; other pages load it with the fonts.
  const appModules = homePage ? undefined : loadApp(language);
  await Promise.race([
    loadFonts(homePage ? LANGUAGE_DETAILS[homePage].fontSample : ANY_TEXT_SAMPLE),
    delay(FONT_WAIT_MS),
  ]);
  document.documentElement.dataset['fonts'] = '';
  if (document.body.firstElementChild) {
    await contentPainted();
  }
  const { prepare, show, tidy } = await (appModules ?? loadApp(language));
  await delay(0);
  prepare();
  show(path, language);
  document.documentElement.dataset['ready'] = '';
  if (screen !== 'read') {
    void watchVisit(screen);
  }
  void tidy();
}

// oxlint-disable-next-line unicorn/prefer-top-level-await -- WebKit font readiness must not wait for this module to finish evaluating.
void boot();
