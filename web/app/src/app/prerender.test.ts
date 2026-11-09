import { describe, expect, it } from 'vitest';

import { LANGUAGES } from '../shared/language/language.ts';
import { homeTemplate } from './prerender.ts';

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
const IMAGE = { path: '/assets/og-image-a1.png', width: 1200, height: 630 };
const ASSETS = { image: IMAGE };

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
    const home = language === 'en' ? '/' : '/cs/';
    expect(page).toContain(`<html lang="${language}">`);
    expect(count(page, /<title>/u)).toBe(1);
    expect(count(page, /<meta name="description"/u)).toBe(1);
    expect(page).not.toContain('<title>Nylzo</title>');
    expect(count(page, /<h1[\s>]/u)).toBe(1);
    expect(page).not.toContain('<body></body>');
    expect(count(page, /<!--nylzo:public-->/u)).toBe(1);
    expect(count(page, /<!--\/nylzo:public-->/u)).toBe(1);
    const block = publicBlock(page);
    expect(block).toContain(`<link rel="canonical" href="__NYLZO_PUBLIC_URL__${home}">`);
    expect(block).toContain('<link rel="alternate" hreflang="en" href="__NYLZO_PUBLIC_URL__/">');
    expect(block).toContain('<link rel="alternate" hreflang="cs" href="__NYLZO_PUBLIC_URL__/cs/">');
    expect(block).toContain('<link rel="alternate" hreflang="x-default" href="__NYLZO_PUBLIC_URL__/">');
    expect(block).toContain(`<meta property="og:image" content="__NYLZO_PUBLIC_URL__${IMAGE.path}">`);
    expect(page.replace(block, '')).not.toContain('__NYLZO_PUBLIC_URL__');
    expect(page).toContain(`<meta property="og:locale" content="${language === 'en' ? 'en_US' : 'cs_CZ'}">`);
    expect(page).toContain('<noscript><style>');
    expect(page).toContain('<link rel="stylesheet" crossorigin href="/assets/style-b2.css">');
    expect(count(page, /-czech-[\w-]+\.woff2" as="font"/u)).toBe(language === 'cs' ? 2 : 0);
    expect(page).not.toContain('latin-ext');
    expect(page).toContain('geist-latin-wght-normal-d4.woff2" as="font"');
  });

  it('writes structured data that parses and matches the visible description and FAQ (TX12)', () => {
    const page = homeTemplate(INDEX, 'cs', ASSETS);
    const [, json = ''] = /<script type="application\/ld\+json">([^<]*)<\/script>/u.exec(page) ?? [];
    const data = JSON.parse(json) as {
      '@graph': {
        '@type': string;
        url?: string;
        description?: string;
        logo?: string;
        mainEntity?: { name: string; acceptedAnswer: { text: string } }[];
      }[];
    };
    const types = data['@graph'].map((node) => node['@type']);
    expect(types).toEqual(['Organization', 'WebSite', 'WebApplication', 'FAQPage']);
    expect(data['@graph'][0]?.logo).toBe('__NYLZO_PUBLIC_URL__/assets/apple-touch-icon-a7.png');
    const description = /<meta name="description" content="([^"]*)"/u.exec(page)?.[1];
    expect(data['@graph'][2]?.description).toBe(description);
    expect(data['@graph'][2]?.url).toBe('__NYLZO_PUBLIC_URL__/cs/');
    const questions = data['@graph'][3]?.mainEntity ?? [];
    expect(questions).toHaveLength(8);
    const visible = (text: string) => page.includes(text.replaceAll('\u{A0}', '&nbsp;')) || page.includes(text);
    expect(questions.filter(({ name, acceptedAnswer }) => !visible(name) || !visible(acceptedAnswer.text))).toEqual([]);
  });

  it('refuses an index it cannot place the page in', () => {
    expect(() => homeTemplate(INDEX.replace('<body></body>', '<body><p></p></body>'), 'en', ASSETS)).toThrow(
      /<body><\/body>/u,
    );
    expect(() => homeTemplate(INDEX.replace('<title>Nylzo</title>', ''), 'en', ASSETS)).toThrow(/title/u);
    expect(() => homeTemplate(INDEX.replaceAll('-czech-', '-other-'), 'cs', ASSETS)).toThrow(/Czech/u);
  });
});
