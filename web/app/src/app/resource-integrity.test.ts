import { runInNewContext } from 'node:vm';
import { parse } from 'parse5';
import { describe, expect, it } from 'vitest';

import { checkScript, checkStyles, FRAGMENT_GUARD, integrity, secureHtml } from '../../scripts/resource-integrity.ts';

const assets = new Map([
  ['/assets/app.js', Buffer.from('import("./start.js")')],
  ['/assets/start.js', Buffer.from('export const start = () => {}')],
  ['/assets/style.css', Buffer.from('body{color:red}')],
  ['/assets/font.woff2', Buffer.from('font')],
]);
const html = `<html><head><meta charset="utf-8"><script src="/assets/app.js" type="module"></script><link rel="stylesheet" href="/assets/style.css"></head><body></body></html>`;

describe('final frontend resource policy', () => {
  it('seals scripts, styles and module preloads after prerendering and adds no nonce or second policy', () => {
    const output = secureHtml(
      html.replace('</head>', '<link rel="modulepreload" href="/assets/start.js"></head>'),
      assets,
    );
    expect(parse(output).nodeName).toBe('#document');
    for (const name of ['app.js', 'style.css', 'start.js']) {
      expect(output).toContain(
        `integrity="${integrity(assets.get(`/assets/${name}`) ?? new Uint8Array())}" crossorigin="anonymous"`,
      );
    }
    expect(output).not.toContain('nonce');
    expect(output).not.toContain('http-equiv');
    expect(output).toContain('<head><meta charset="utf-8"><script type="importmap">');
    expect(output).toContain(`</script><script>${FRAGMENT_GUARD}</script>`);
    expect(output.indexOf(FRAGMENT_GUARD)).toBeLessThan(output.indexOf('src="/assets/app.js"'));
  });

  it.each([
    'https://evil.invalid/a.js',
    '//evil.invalid/a.js',
    '/assets/../a.js',
    '/assets/app.js?new=1',
    'data:text/javascript,alert(1)',
    'https&#58;//evil.invalid/a.js',
  ])('rejects unbundled HTML script URL %s', (url) => {
    expect(() =>
      secureHtml(
        html.replace('/assets/app.js', () => url),
        assets,
      ),
    ).toThrow(/resource integrity/);
  });

  it('rejects executable inline scripts and import maps but preserves inert JSON-LD', () => {
    for (const script of ['<script>alert(1)</script>', '<script type="importmap">{}</script>']) {
      expect(() =>
        secureHtml(
          html.replace('</head>', () => `${script}</head>`),
          assets,
        ),
      ).toThrow(/inline scripts/);
    }
    expect(
      secureHtml(
        html.replace('</head>', () => `<script type="application/ld+json">{}</script></head>`),
        assets,
      ),
    ).toContain('application/ld+json');
  });

  it('also seals and validates fallback styles when JavaScript is disabled', () => {
    const page = html.replace(
      '</head>',
      () => `<noscript><link rel="stylesheet" href="/assets/style.css"></noscript></head>`,
    );
    expect(secureHtml(page, assets).match(/integrity="sha384-/g)).toHaveLength(3);
    expect(() => secureHtml(page.replaceAll('/assets/style.css', 'https://evil.invalid/style.css'), assets)).toThrow(
      /resource integrity/,
    );
  });

  it('requires a completely local literal module graph, including static template literals', () => {
    for (const code of [
      'import "./start.js";',
      'export * from "./start.js";',
      'import("./start.js")',
      'import(`./start.js`)',
    ]) {
      expect(() => checkScript(code, '/assets/app.js', assets)).not.toThrow();
    }
    for (const code of [
      'import("https://evil.invalid/a.js")',
      'import(location.hash)',
      'import(`./${name}.js`)',
      'export * from "missing-package"',
      'import("./missing.js")',
    ]) {
      expect(() => checkScript(code, '/assets/app.js', assets)).toThrow(/resource integrity/);
    }
    expect(() => checkScript('const example = "import(location.hash)"', '/assets/app.js', assets)).not.toThrow();
  });

  it('parses CSS URLs and forbids remote URLs and all remaining imports', () => {
    expect(() => checkStyles('@font-face{src:url(./font.woff2)}', '/assets/style.css', assets)).not.toThrow();
    for (const css of [
      '@import "./style.css";',
      '@font-face{src:url(https://evil.invalid/font.woff2)}',
      'body{background:url(//evil.invalid/x)}',
      String.raw`body{background:url(h\74tps://evil.invalid/x)}`,
    ]) {
      expect(() => checkStyles(css, '/assets/style.css', assets)).toThrow(/resource integrity/);
      expect(() =>
        secureHtml(
          html.replace('</head>', () => `<style>${css}</style></head>`),
          assets,
        ),
      ).toThrow(/resource integrity/);
    }
  });
});

function runGuard(href: string) {
  const url = new URL(href);
  const session = new Map<string, string>();
  const replaced: string[] = [];
  const context = {
    location: { pathname: url.pathname, search: url.search, hash: url.hash },
    history: {
      replaceState: (_state: unknown, _title: string, address: string) => {
        replaced.push(address);
      },
    },
    sessionStorage: { setItem: (key: string, value: string) => session.set(key, value) },
  };
  Object.assign(context, { globalThis: context });
  runInNewContext(FRAGMENT_GUARD, context);
  const take = Reflect.get(context, 'nylzoFragment') as (() => string) | undefined;
  return { replaced, session, take, context };
}

describe('the fragment guard at the start of every page (W2-04)', () => {
  it('takes the fragment off a read page before anything else loads and hands it over once', () => {
    const { replaced, session, take, context } = runGuard('https://nylzo.test/m/AAAAAAAAAAAAAAAAAAAAAA?x=1#1.0.secret');
    expect(replaced).toEqual(['/m/AAAAAAAAAAAAAAAAAAAAAA?x=1']);
    expect(session).toEqual(new Map([['nylzo:dropped:AAAAAAAAAAAAAAAAAAAAAA', '1']]));
    expect(take?.()).toBe('#1.0.secret');
    expect(Reflect.get(context, 'nylzoFragment')).toBeUndefined();
    expect(take?.()).toBe('');
  });

  it('leaves other pages and read pages without a fragment alone', () => {
    for (const href of ['https://nylzo.test/#how', 'https://nylzo.test/m/AAAAAAAAAAAAAAAAAAAAAA']) {
      const { replaced, session, take } = runGuard(href);
      expect(replaced).toEqual([]);
      expect(session.size).toBe(0);
      expect(take).toBeUndefined();
    }
  });
});
