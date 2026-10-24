import { describe, expect, it } from 'vitest';

import { homeTemplate, LANGUAGES } from '../src/prerender.ts';

const INDEX = `<!doctype html>
<html lang="cs">
<head>
<meta charset="utf-8">
<meta name="description" content="Nylzo">
<title>Nylzo</title>
<script type="module" crossorigin src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script>
</head>
<body></body>
</html>
`;
const IMAGE = { path: '/assets/og-image-a1.png', width: 1200, height: 630 };

function count(page: string, pattern: RegExp): number {
  return page.match(new RegExp(pattern, 'g'))?.length ?? 0;
}

function publicBlock(page: string): string {
  const [, block = ''] = /<!--nylzo:public-->([\s\S]*?)<!--\/nylzo:public-->/.exec(page) ?? [];
  return block;
}

describe('homeTemplate', () => {
  it.each(LANGUAGES)('renders the %s home page with its own head and content', (language) => {
    const page = homeTemplate(INDEX, language, IMAGE);
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
  });

  it('writes structured data that parses and matches the visible description', () => {
    const page = homeTemplate(INDEX, 'cs', IMAGE);
    const [, json = ''] = /<script type="application\/ld\+json" nonce="__NYLZO_CSP_NONCE__">([^<]*)<\/script>/.exec(page) ?? [];
    const data = JSON.parse(json) as { '@graph': { '@type': string; url: string; description?: string }[] };
    const types = data['@graph'].map((node) => node['@type']);
    expect(types).toEqual(['WebSite', 'WebApplication']);
    const description = /<meta name="description" content="([^"]*)"/.exec(page)?.[1];
    expect(data['@graph'][1]?.description).toBe(description);
    expect(data['@graph'][1]?.url).toBe('__NYLZO_PUBLIC_URL__/cs/');
  });

  it('refuses an index it cannot place the page in', () => {
    expect(() => homeTemplate(INDEX.replace('<body></body>', '<body><p></p></body>'), 'en', IMAGE)).toThrow(/<body><\/body>/);
    expect(() => homeTemplate(INDEX.replace('<title>Nylzo</title>', ''), 'en', IMAGE)).toThrow(/title/);
  });
});
