import { render } from 'svelte/server';

import App from './App.svelte';
import { createI18n, type Language } from './lib/i18n.svelte.ts';
import { HOME_PATHS } from './lib/pages.ts';

export const NONCE_PLACEHOLDER = '__NYLZO_CSP_NONCE__';
export const PUBLIC_URL_PLACEHOLDER = '__NYLZO_PUBLIC_URL__';
export const LANGUAGES: readonly Language[] = ['en', 'cs'];

const PUBLIC_START = '<!--nylzo:public-->';
const PUBLIC_END = '<!--/nylzo:public-->';
const OG_LOCALES: Readonly<Record<Language, string>> = { en: 'en_US', cs: 'cs_CZ' };

export interface OgImage {
  readonly path: string;
  readonly width: number;
  readonly height: number;
}

function escapeAttribute(value: string): string {
  return value.replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
}

function jsonScript(value: unknown): string {
  return JSON.stringify(value).replaceAll('<', '\\u003c').replaceAll('>', '\\u003e').replaceAll('&', '\\u0026');
}

function absolute(path: string): string {
  return `${PUBLIC_URL_PLACEHOLDER}${path}`;
}

function replaceOnce(source: string, pattern: string | RegExp, replacement: string): string {
  const matches = typeof pattern === 'string' ? source.split(pattern).length - 1 : (source.match(new RegExp(pattern, 'g')) ?? []).length;
  if (matches !== 1) {
    throw new Error(`prerender: expected one match of ${String(pattern)}, found ${matches}`);
  }
  return source.replace(pattern, () => replacement);
}

function structuredData(language: Language, description: string): unknown {
  const home = absolute(HOME_PATHS[language]);
  return {
    '@context': 'https://schema.org',
    '@graph': [
      { '@type': 'WebSite', '@id': `${absolute('/')}#website`, name: 'Nylzo', url: absolute('/'), inLanguage: LANGUAGES },
      {
        '@type': 'WebApplication',
        '@id': `${absolute('/')}#app`,
        name: 'Nylzo',
        url: home,
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
  const { t } = createI18n(language);
  const other = LANGUAGES.filter((l) => l !== language);
  const meta = (property: string, content: string) => `<meta property="${property}" content="${escapeAttribute(content)}">`;
  const shared = [
    meta('og:type', 'website'),
    meta('og:site_name', 'Nylzo'),
    meta('og:title', t('seo.title')),
    meta('og:description', t('seo.description')),
    meta('og:locale', OG_LOCALES[language]),
    ...other.map((l) => meta('og:locale:alternate', OG_LOCALES[l])),
    '<meta name="twitter:card" content="summary_large_image">',
  ];
  const alternates = [
    ...LANGUAGES.map((l) => `<link rel="alternate" hreflang="${l}" href="${absolute(HOME_PATHS[l])}">`),
    `<link rel="alternate" hreflang="x-default" href="${absolute(HOME_PATHS.en)}">`,
  ];
  const located = [
    `<link rel="canonical" href="${absolute(HOME_PATHS[language])}">`,
    ...alternates,
    meta('og:url', absolute(HOME_PATHS[language])),
    meta('og:image', absolute(image.path)),
    meta('og:image:type', 'image/png'),
    meta('og:image:width', String(image.width)),
    meta('og:image:height', String(image.height)),
    meta('og:image:alt', t('seo.image')),
    `<script type="application/ld+json" nonce="${NONCE_PLACEHOLDER}">${jsonScript(structuredData(language, t('seo.description')))}</script>`,
  ];
  return [...shared, PUBLIC_START, ...located, PUBLIC_END].join('\n');
}

const REVEAL_WITHOUT_SCRIPT = `<noscript><style nonce="${NONCE_PLACEHOLDER}">html:not([data-ready]) body{visibility:visible}</style></noscript>`;

export function homeTemplate(index: string, language: Language, image: OgImage): string {
  const { head, body } = render(App, {
    props: { start: { path: HOME_PATHS[language], language } },
    csp: { nonce: NONCE_PLACEHOLDER },
  });
  let page = replaceOnce(index, /<html lang="[a-z]+">/, `<html lang="${language}">`);
  page = replaceOnce(page, /<meta name="description"[^>]*>\n?/, '');
  page = replaceOnce(page, /<title>[^<]*<\/title>/, `${head.trim()}\n${seoHead(language, image)}`);
  page = replaceOnce(page, '</head>', `${REVEAL_WITHOUT_SCRIPT}\n</head>`);
  return replaceOnce(page, '<body></body>', `<body>${body}</body>`);
}
