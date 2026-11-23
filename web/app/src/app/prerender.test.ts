import { render } from 'svelte/server';
import { describe, expect, it, vi } from 'vitest';

import { FAQ_KEYS } from '../features/create/sections/faq-entries.ts';
import { STATUS_PAGES } from '../features/errors/status-page.ts';
import { homePath, languageDetails, LANGUAGES } from '../shared/language/language.ts';
import { Translator } from '../shared/language/translator.svelte.ts';
import App from './App.svelte';
import { agentsMarkdown, homeTemplate, homeTemplates, llmsText, skeletonIndex, statusTemplates } from './prerender.ts';

const browserOnlyComponent = vi.hoisted(() => (): never => {
  throw new ReferenceError('document is not defined');
});

vi.mock('../features/read/Read.svelte', () => ({ default: browserOnlyComponent }));
vi.mock('../features/sent/Sent.svelte', () => ({ default: browserOnlyComponent }));
vi.mock('../features/transparency/Transparency.svelte', () => ({ default: browserOnlyComponent }));
vi.mock('../features/create/created/CreatedPanel.svelte', () => ({ default: browserOnlyComponent }));

const INDEX = `<!doctype html>
<html lang="cs">
<head>
<meta charset="utf-8">
<meta name="description" content="Nylzo">
<title>Nylzo</title>
<script type="module" crossorigin src="/assets/index-a1.js"></script>
<link rel="apple-touch-icon" href="/assets/apple-touch-icon-a7.png">
<link rel="stylesheet" crossorigin href="/assets/style-b2.css">
<link rel="preload" href="/assets/geist-czech-c3.woff2" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="/assets/geist-latin-ext-wght-normal-f6.woff2" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="/assets/geist-latin-wght-normal-d4.woff2" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="/assets/sora-czech-e5.woff2" as="font" type="font/woff2" crossorigin>
</head>
<body></body>
</html>
`;
const IMAGE = { path: '/assets/og-image-a1.jpg', width: 1200, height: 630 };
const ASSETS = { image: IMAGE };
const WITH_CZECH_LETTERS = new Set(['cs', 'sk']);
const WITH_EXTENDED_LETTERS = new Set(['pl', 'sk']);

function count(page: string, pattern: RegExp): number {
  return page.match(new RegExp(pattern, 'gu'))?.length ?? 0;
}

function publicBlock(page: string): string {
  const [, block = ''] = /<!--nylzo:public-->([\s\S]*?)<!--\/nylzo:public-->/u.exec(page) ?? [];
  return block;
}

describe('homeTemplate', () => {
  it.each(LANGUAGES)('renders the %s home page with its own head and content', (language) => {
    const page = homeTemplate(INDEX, language, ASSETS);
    const home = homePath(language);
    expect(page).toContain(`<html lang="${language}">`);
    expect(count(page, /<title>/u)).toBe(1);
    expect(page).toContain(`<title>${new Translator(language).translate('seo.title')}</title>`);
    expect(count(page, /<meta name="description"/u)).toBe(1);
    expect(page).not.toContain('<title>Nylzo</title>');
    expect(count(page, /<h1[\s>]/u)).toBe(1);
    expect(page).not.toContain('<body></body>');
    expect(count(page, /<!--nylzo:public-->/u)).toBe(1);
    expect(count(page, /<!--\/nylzo:public-->/u)).toBe(1);
    const block = publicBlock(page);
    expect(block).toContain(`<link rel="canonical" href="__NYLZO_PUBLIC_URL__${home}">`);
    for (const alternate of LANGUAGES) {
      expect(block).toContain(
        `<link rel="alternate" hreflang="${alternate}" href="__NYLZO_PUBLIC_URL__${homePath(alternate)}">`,
      );
    }
    expect(block).toContain('<link rel="alternate" hreflang="x-default" href="__NYLZO_PUBLIC_URL__/">');
    expect(block).toContain(`<meta property="og:image" content="__NYLZO_PUBLIC_URL__${IMAGE.path}">`);
    expect(page.replace(block, '')).not.toContain('__NYLZO_PUBLIC_URL__');
    expect(page).toContain(`<meta property="og:locale" content="${languageDetails(language).openGraphLocale}">`);
    expect(page).toContain('<noscript><style>');
    const hidden = page.indexOf('<style>html:not([data-fonts]) body{content-visibility:hidden}</style>');
    expect(hidden).toBeGreaterThan(-1);
    expect(hidden).toBeLessThan(page.indexOf('<noscript><style>'));
    expect(page).toContain('<link rel="stylesheet" crossorigin href="/assets/style-b2.css">');
    expect(count(page, /-czech-[\w-]+\.woff2" as="font"/u)).toBe(WITH_CZECH_LETTERS.has(language) ? 2 : 0);
    expect(count(page, /latin-ext/u)).toBe(WITH_EXTENDED_LETTERS.has(language) ? 1 : 0);
    expect(page).toContain('geist-latin-wght-normal-d4.woff2" as="font"');
  });

  it('writes structured data that parses and matches the visible description and FAQ (TX12)', () => {
    const page = homeTemplate(INDEX, 'cs', ASSETS);
    const [, json = ''] = /<script type="application\/ld\+json">([^<]*)<\/script>/u.exec(page) ?? [];
    const data: unknown = JSON.parse(json);
    const description = /<meta name="description" content="([^"]*)"/u.exec(page)?.[1];
    const visible = (text: unknown) =>
      typeof text === 'string' && (page.includes(text.replaceAll('\u{A0}', '&nbsp;')) || page.includes(text));
    const visibleText: unknown = expect.toSatisfy(visible);
    expect(data).toMatchObject({
      '@graph': [
        { '@type': 'Organization', logo: '__NYLZO_PUBLIC_URL__/assets/apple-touch-icon-a7.png' },
        { '@type': 'WebSite' },
        { '@type': 'WebApplication', description, url: '__NYLZO_PUBLIC_URL__/cs/' },
        {
          '@type': 'FAQPage',
          mainEntity: Array.from({ length: 9 }, () => ({ name: visibleText, acceptedAnswer: { text: visibleText } })),
        },
      ],
    });
  });

  it('refuses an index it cannot place the page in', () => {
    expect(() => homeTemplate(INDEX.replace('<body></body>', '<body><p></p></body>'), 'en', ASSETS)).toThrow(
      /<body><\/body>/u,
    );
    expect(() => homeTemplate(INDEX.replace('<title>Nylzo</title>', ''), 'en', ASSETS)).toThrow(/title/u);
    expect(() => homeTemplate(INDEX.replaceAll('-czech-', '-other-'), 'cs', ASSETS)).toThrow(/Czech/u);
    expect(() => homeTemplate(INDEX.replace(/<link rel="apple-touch-icon"[^>]*>/u, ''), 'en', ASSETS)).toThrow(
      /apple touch icon/u,
    );
  });

  it('renders one home page for every language', () => {
    const pages = homeTemplates(INDEX, ASSETS);
    expect([...pages.keys()]).toStrictEqual(LANGUAGES);
    for (const [language, page] of pages) {
      expect(page).toBe(homeTemplate(INDEX, language, ASSETS));
    }
  });
});

describe('statusTemplates', () => {
  it('renders every status page in every language without loading the app', () => {
    const pages = statusTemplates(INDEX);
    const names = STATUS_PAGES.flatMap((page) => LANGUAGES.map((language) => `${page}.${language}.html`));
    expect([...pages.keys()]).toStrictEqual(names);
    for (const language of LANGUAGES) {
      const page = pages.get(`error.${language}.html`) ?? '';
      expect(page).toContain(`<html lang="${language}" data-fonts>`);
      expect(count(page, /<title>/u)).toBe(1);
      expect(page).not.toContain('<title>Nylzo</title>');
      expect(page).not.toContain('<meta name="description"');
      expect(page).not.toContain('<script type="module"');
      expect(page).not.toContain('<body></body>');
      expect(page).toContain('<link rel="stylesheet" crossorigin href="/assets/style-b2.css">');
      expect(count(page, /-czech-[\w-]+\.woff2" as="font"/u)).toBe(WITH_CZECH_LETTERS.has(language) ? 2 : 0);
    }
  });

  it('drops the module preloads along with the module script', () => {
    const index = INDEX.replace(
      '</head>',
      '<link rel="modulepreload" crossorigin href="/assets/chunk-c1.js">\n</head>',
    );
    for (const page of statusTemplates(index).values()) {
      expect(page).not.toContain('modulepreload');
    }
  });
});

describe('skeletonIndex', () => {
  it('shows the skeleton without waiting for fonts and hides its spinner without script', () => {
    const page = skeletonIndex(INDEX);
    expect(page).toContain('<html data-fonts lang="cs">');
    expect(page).toMatch(/<body>(?:<!--\[-->)?<div[^>]*data-skeleton/u);
    expect(page.indexOf('<noscript><style>[data-skeleton]')).toBeLessThan(page.indexOf('</head>'));
    expect(page).toContain('<script type="module" crossorigin src="/assets/index-a1.js"></script>');
  });

  it('refuses an index that already has a body', () => {
    expect(() => skeletonIndex(INDEX.replace('<body></body>', '<body><p></p></body>'))).toThrow(/<body><\/body>/u);
  });
});

describe('agentsMarkdown', () => {
  it('writes the agent guide in English, whatever language the build serves first', () => {
    const { translate } = new Translator('en');
    const [heading] = agentsMarkdown().split('\n', 1);
    expect(heading).toBe(`# ${translate('agents.title')}`);
  });
});

describe('llmsText', () => {
  it('lists every English FAQ answer with plain spaces in the llms.txt format', () => {
    const text = llmsText();
    const { translate } = new Translator('en');
    expect(text).toMatch(/^# Nylzo\n\n> /u);
    expect(text).not.toContain('\u{A0}');
    for (const key of FAQ_KEYS) {
      expect(text).toContain(`### ${translate(`${key}.question`).replaceAll('\u{A0}', ' ')}`);
    }
  });
});

describe('the app on the server', () => {
  const { translate } = new Translator('en');

  it.each([
    ['/agents', 'agents.title'],
    ['/terms', 'terms.title'],
    ['/nothing', 'error.notFound.title'],
  ] as const)('renders the screen of %s', (path, heading) => {
    const { body } = render(App, { props: { start: { path, language: 'en' } } });
    const title = RegExp.escape(translate(heading));
    expect(body).toMatch(new RegExp(`<h1[^>]*>\\s*${title}\\s*</h1>`, 'u'));
    expect(body).not.toContain('role="alert"');
  });

  it.each(['/sent', '/transparency', '/r'])('renders the crash notice for %s, which needs a browser', (path) => {
    const { body } = render(App, { props: { start: { path, language: 'en' } }, transformError: (error) => error });
    expect(body).toMatch(/<p role="alert"[^>]*>[^<]+<\/p>/u);
  });
});
