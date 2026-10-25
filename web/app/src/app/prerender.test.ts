import { describe, expect, it } from 'vitest';

import { homeTemplate, LANGUAGES, splitFontFaces } from './prerender.ts';

const INDEX = `<!doctype html>
<html lang="cs">
<head>
<meta charset="utf-8">
<meta name="description" content="Nylzo">
<title>Nylzo</title>
<script type="module" crossorigin src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script>
<link rel="stylesheet" crossorigin href="/assets/style-b2.css" nonce="__NYLZO_CSP_NONCE__">
<link rel="preload" href="/assets/geist-czech-c3.woff2" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="/assets/geist-latin-ext-wght-normal-f6.woff2" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="/assets/geist-latin-wght-normal-d4.woff2" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="/assets/sora-czech-e5.woff2" as="font" type="font/woff2" crossorigin>
</head>
<body></body>
</html>
`;
const IMAGE = { path: '/assets/og-image-a1.png', width: 1200, height: 630 };
const CSS = 'body{color:#fff}@font-face{src:url(/assets/geist-czech-c3.woff2)}h1{margin:0}';
const STYLES = {
  stylesheet: '/assets/style-b2.css',
  fontStylesheet: '/assets/fonts-f7.css',
  inline: splitFontFaces(CSS).inline,
};
const ASSETS = { image: IMAGE, styles: STYLES };

function count(page: string, pattern: RegExp): number {
  return page.match(new RegExp(pattern, 'g'))?.length ?? 0;
}

function publicBlock(page: string): string {
  const [, block = ''] = /<!--nylzo:public-->([\s\S]*?)<!--\/nylzo:public-->/.exec(page) ?? [];
  return block;
}

describe('homeTemplate', () => {
  it.each(LANGUAGES)('renders the %s home page with its own head and content', (language) => {
    const page = homeTemplate(INDEX, language, ASSETS);
    const home = language === 'en' ? '/' : '/cs/';
    expect(page).toContain(`<html lang="${language}">`);
    expect(count(page, /<title>/)).toBe(1);
    expect(count(page, /<meta name="description"/)).toBe(1);
    expect(page).not.toContain('<title>Nylzo</title>');
    expect(count(page, /<h1[\s>]/)).toBe(1);
    expect(page).not.toContain('<body></body>');
    expect(count(page, /<!--nylzo:public-->/)).toBe(1);
    expect(count(page, /<!--\/nylzo:public-->/)).toBe(1);
    const block = publicBlock(page);
    expect(block).toContain(`<link rel="canonical" href="__NYLZO_PUBLIC_URL__${home}">`);
    expect(block).toContain('<link rel="alternate" hreflang="en" href="__NYLZO_PUBLIC_URL__/">');
    expect(block).toContain('<link rel="alternate" hreflang="cs" href="__NYLZO_PUBLIC_URL__/cs/">');
    expect(block).toContain('<link rel="alternate" hreflang="x-default" href="__NYLZO_PUBLIC_URL__/">');
    expect(block).toContain(`<meta property="og:image" content="__NYLZO_PUBLIC_URL__${IMAGE.path}">`);
    expect(page.replace(block, '')).not.toContain('__NYLZO_PUBLIC_URL__');
    expect(page).toContain(`<meta property="og:locale" content="${language === 'en' ? 'en_US' : 'cs_CZ'}">`);
    expect(page).toContain('<noscript><style nonce="__NYLZO_CSP_NONCE__">');
    expect(page).toContain(
      '<link rel="stylesheet" crossorigin href="/assets/fonts-f7.css" nonce="__NYLZO_CSP_NONCE__">',
    );
    expect(page).toContain('<style nonce="__NYLZO_CSP_NONCE__">body{color:#fff}h1{margin:0}</style>');
    expect(page).not.toContain('style-b2.css');
    expect(count(page, /-czech-[\w-]+\.woff2" as="font"/)).toBe(language === 'cs' ? 2 : 0);
    expect(page).not.toContain('latin-ext');
    expect(page).toContain('geist-latin-wght-normal-d4.woff2" as="font"');
  });

  it('writes structured data that parses and matches the visible description', () => {
    const page = homeTemplate(INDEX, 'cs', ASSETS);
    const [, json = ''] =
      /<script type="application\/ld\+json" nonce="__NYLZO_CSP_NONCE__">([^<]*)<\/script>/.exec(page) ?? [];
    const data = JSON.parse(json) as { '@graph': { '@type': string; url: string; description?: string }[] };
    const types = data['@graph'].map((node) => node['@type']);
    expect(types).toEqual(['WebSite', 'WebApplication']);
    const description = /<meta name="description" content="([^"]*)"/.exec(page)?.[1];
    expect(data['@graph'][1]?.description).toBe(description);
    expect(data['@graph'][1]?.url).toBe('__NYLZO_PUBLIC_URL__/cs/');
  });

  it('refuses an index it cannot place the page in', () => {
    expect(() => homeTemplate(INDEX.replace('<body></body>', '<body><p></p></body>'), 'en', ASSETS)).toThrow(
      /<body><\/body>/,
    );
    expect(() => homeTemplate(INDEX.replace('<title>Nylzo</title>', ''), 'en', ASSETS)).toThrow(/title/);
    expect(() =>
      homeTemplate(INDEX, 'en', { image: IMAGE, styles: { ...STYLES, stylesheet: '/assets/other.css' } }),
    ).toThrow(/stylesheet/);
    expect(() =>
      homeTemplate(INDEX, 'en', { image: IMAGE, styles: { ...STYLES, inline: 'a{}</style><script>' } }),
    ).toThrow(/stylesheet/);
    expect(() => homeTemplate(INDEX.replaceAll('-czech-', '-other-'), 'cs', ASSETS)).toThrow(/Czech/);
    expect(() => splitFontFaces('body{}')).toThrow(/fonts/);
  });
});
