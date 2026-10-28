import { render } from 'svelte/server';

import { type Language, LANGUAGES } from '../shared/language/language.ts';
import { Translator } from '../shared/language/translator.svelte.ts';
import App from './App.svelte';
import { HOME_PATHS } from './pages.ts';

interface OgImage {
  readonly path: string;
  readonly width: number;
  readonly height: number;
}

interface HomeStyles {
  readonly stylesheet: string;
  readonly fontStylesheet: string;
  readonly inline: string;
}

export interface HomeAssets {
  readonly image: OgImage;
  readonly styles: HomeStyles;
}

const NONCE_PLACEHOLDER = '__NYLZO_CSP_NONCE__';
const PUBLIC_URL_PLACEHOLDER = '__NYLZO_PUBLIC_URL__';
const PUBLIC_START = '<!--nylzo:public-->';
const PUBLIC_END = '<!--/nylzo:public-->';
const OG_LOCALES: Readonly<Record<Language, string>> = { en: 'en_US', cs: 'cs_CZ' };
const REVEAL_WITHOUT_SCRIPT = `<noscript><style nonce="${NONCE_PLACEHOLDER}">html:not([data-fonts]) body{content-visibility:visible}</style></noscript>`;
const STYLESHEET = /<link rel="stylesheet"[^>]* href="([^"]+)"[^>]*>/;
const CZECH_FONT_PRELOAD = /<link rel="preload" href="\/assets\/[\w-]+-czech-[\w-]+\.woff2"[^>]*>\n?/g;
const EXTENDED_FONT_PRELOAD = /<link rel="preload" href="\/assets\/[\w-]+-latin-ext-[\w-]+\.woff2"[^>]*>\n?/g;
const FONT_FACE = /@font-face\{[^}]*\}/g;

function escapeAttribute(value: string): string {
  return value.replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
}

function jsonScript(value: unknown): string {
  return JSON.stringify(value)
    .replaceAll('<', String.raw`\u003c`)
    .replaceAll('>', String.raw`\u003e`)
    .replaceAll('&', String.raw`\u0026`);
}

function meta(property: string, content: string): string {
  return `<meta property="${property}" content="${escapeAttribute(content)}">`;
}

function absolute(path: string): string {
  return `${PUBLIC_URL_PLACEHOLDER}${path}`;
}

function matchCount(source: string, pattern: RegExp): number {
  return source.match(new RegExp(pattern, 'g'))?.length ?? 0;
}

function replaceOnce(source: string, pattern: RegExp | string, replacement: string): string {
  const matches = typeof pattern === 'string' ? source.split(pattern).length - 1 : matchCount(source, pattern);
  if (matches !== 1) {
    throw new Error(`prerender: expected one match of ${String(pattern)}, found ${matches}`);
  }
  return source.replace(pattern, () => replacement);
}

function structuredData(language: Language, description: string): unknown {
  return {
    '@context': 'https://schema.org',
    '@graph': [
      {
        '@type': 'WebSite',
        '@id': `${absolute('/')}#website`,
        name: 'Nylzo',
        url: absolute('/'),
        inLanguage: LANGUAGES,
      },
      {
        '@type': 'WebApplication',
        '@id': `${absolute('/')}#app`,
        name: 'Nylzo',
        url: absolute(HOME_PATHS[language]),
        description,
        inLanguage: language,
        applicationCategory: 'SecurityApplication',
        operatingSystem: 'Any',
        browserRequirements: 'Requires JavaScript and the Web Crypto API',
        license: 'https://opensource.org/license/mit',
        isPartOf: { '@id': `${absolute('/')}#website` },
      },
    ],
  };
}

function seoHead(language: Language, image: OgImage): string {
  const { translate } = new Translator(language);
  const shared = [
    meta('og:type', 'website'),
    meta('og:site_name', 'Nylzo'),
    meta('og:title', translate('seo.title')),
    meta('og:description', translate('seo.description')),
    meta('og:locale', OG_LOCALES[language]),
    ...LANGUAGES.filter((other) => other !== language).map((other) => meta('og:locale:alternate', OG_LOCALES[other])),
    '<meta name="twitter:card" content="summary_large_image">',
  ];
  const located = [
    `<link rel="canonical" href="${absolute(HOME_PATHS[language])}">`,
    ...LANGUAGES.map(
      (alternate) => `<link rel="alternate" hreflang="${alternate}" href="${absolute(HOME_PATHS[alternate])}">`,
    ),
    `<link rel="alternate" hreflang="x-default" href="${absolute(HOME_PATHS.en)}">`,
    meta('og:url', absolute(HOME_PATHS[language])),
    meta('og:image', absolute(image.path)),
    meta('og:image:type', 'image/png'),
    meta('og:image:width', String(image.width)),
    meta('og:image:height', String(image.height)),
    meta('og:image:alt', translate('seo.image')),
    `<script type="application/ld+json" nonce="${NONCE_PLACEHOLDER}">${jsonScript(structuredData(language, translate('seo.description')))}</script>`,
  ];
  return [...shared, PUBLIC_START, ...located, PUBLIC_END].join('\n');
}

export function splitFontFaces(css: string): { readonly fontFaces: string; readonly inline: string } {
  const fontFaces = css.match(FONT_FACE)?.join('') ?? '';
  if (fontFaces === '') {
    throw new Error('prerender: the stylesheet declares no fonts');
  }
  return { fontFaces, inline: css.replaceAll(FONT_FACE, '') };
}

function inlineStyles(page: string, { stylesheet, fontStylesheet, inline }: HomeStyles): string {
  if (STYLESHEET.exec(page)?.[1] !== stylesheet || inline.includes('</style')) {
    throw new Error(`prerender: cannot inline the stylesheet ${stylesheet}`);
  }
  return replaceOnce(
    page,
    STYLESHEET,
    `<link rel="stylesheet" crossorigin href="${fontStylesheet}" nonce="${NONCE_PLACEHOLDER}">\n<style nonce="${NONCE_PLACEHOLDER}">${inline}</style>`,
  );
}

function fontPreloads(page: string, language: Language): string {
  if (matchCount(page, CZECH_FONT_PRELOAD) === 0 || matchCount(page, EXTENDED_FONT_PRELOAD) === 0) {
    throw new Error('prerender: the index preloads no Czech or extended Latin fonts');
  }
  const home = page.replaceAll(EXTENDED_FONT_PRELOAD, '');
  return language === 'cs' ? home : home.replaceAll(CZECH_FONT_PRELOAD, '');
}

export function homeTemplate(index: string, language: Language, { image, styles }: HomeAssets): string {
  const { head, body } = render(App, {
    props: { start: { path: HOME_PATHS[language], language } },
    csp: { nonce: NONCE_PLACEHOLDER },
  });
  let page = replaceOnce(index, /<html lang="[a-z]+">/, `<html lang="${language}">`);
  page = replaceOnce(page, /<meta\s+name="description"[^>]*>\s*/, '');
  page = replaceOnce(page, /<title>[^<]*<\/title>/, `${head.trim()}\n${seoHead(language, image)}`);
  page = replaceOnce(page, '</head>', `${REVEAL_WITHOUT_SCRIPT}\n</head>`);
  page = fontPreloads(inlineStyles(page, styles), language);
  return replaceOnce(page, '<body></body>', `<body>${body}</body>`);
}

export function homeTemplates(index: string, assets: HomeAssets): ReadonlyMap<Language, string> {
  return new Map(LANGUAGES.map((language) => [language, homeTemplate(index, language, assets)]));
}
