import { runInNewContext } from 'node:vm';

import { parse } from 'parse5';
import { describe, expect, it } from 'vitest';

import { checkScript, checkStyles, FRAGMENT_GUARD, integrity, secureHtml } from './resource-integrity.ts';

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

  it('gives a page that loads no script only the fragment guard, without an import map', () => {
    const output = secureHtml(html.replace('<script src="/assets/app.js" type="module"></script>', ''), assets);
    expect(output).not.toContain('importmap');
    expect(output).toContain(`<head><meta charset="utf-8"><script>${FRAGMENT_GUARD}</script>`);
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
    ).toThrow(/resource integrity/u);
  });

  it('rejects executable inline scripts and import maps but preserves inert JSON-LD', () => {
    for (const script of ['<script>alert(1)</script>', '<script type="importmap">{}</script>']) {
      expect(() =>
        secureHtml(
          html.replace('</head>', () => `${script}</head>`),
          assets,
        ),
      ).toThrow(/inline scripts/u);
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
    expect(secureHtml(page, assets).match(/integrity="sha384-/gu)).toHaveLength(3);
    expect(() => secureHtml(page.replaceAll('/assets/style.css', 'https://evil.invalid/style.css'), assets)).toThrow(
      /resource integrity/u,
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
      expect(() => checkScript(code, '/assets/app.js', assets)).toThrow(/resource integrity/u);
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
      expect(() => checkStyles(css, '/assets/style.css', assets)).toThrow(/resource integrity/u);
      expect(() =>
        secureHtml(
          html.replace('</head>', () => `<style>${css}</style></head>`),
          assets,
        ),
      ).toThrow(/resource integrity/u);
    }
  });
});

function runGuard(href: string) {
  const url = new URL(href);
  const session = new Map<string, string>();
  const replaced: string[] = [];
  const listeners = new Map<string, () => void>();
  const location = { hostname: url.hostname, pathname: url.pathname, search: url.search, hash: url.hash };
  const context = {
    location,
    history: {
      replaceState: (_state: unknown, _title: string, address: string) => {
        replaced.push(address);
      },
    },
    sessionStorage: { setItem: (key: string, value: string) => session.set(key, value) },
    addEventListener: (type: string, listener: () => void) => listeners.set(type, listener),
  };
  Object.assign(context, { globalThis: context });
  runInNewContext(FRAGMENT_GUARD, context);
  const take = Reflect.get(context, 'nylzoFragment') as (() => string) | undefined;
  const changeHash = (next: string) => {
    const changed = new URL(next, url);
    Object.assign(location, { pathname: changed.pathname, search: changed.search, hash: changed.hash });
    listeners.get('hashchange')?.();
  };
  return { replaced, session, take, context, changeHash };
}

describe('the fragment guard at the start of every page (W2-04)', () => {
  it.each([
    ['/r?x=1', '#2.0.id.secret.hash', 'r'],
    ['/m/AAAAAAAAAAAAAAAAAAAAAA?x=1', '#1.0.secret', 'AAAAAAAAAAAAAAAAAAAAAA'],
  ])(
    'takes the fragment off the read page %s before anything else loads and hands it over once',
    (page, fragment, key) => {
      const { replaced, session, take, context } = runGuard(`https://nylzo.test${page}${fragment}`);
      expect(replaced).toEqual([page]);
      expect(session).toEqual(new Map([[`nylzo:dropped:${key}`, '1']]));
      expect(take?.()).toBe(fragment);
      expect(Reflect.get(context, 'nylzoFragment')).toBeUndefined();
      expect(take?.()).toBe('');
    },
  );

  it.each([
    ['/open', '#ABCDEFGHIJKLMNOPQRSTUVWXYZ'],
    ['/m/short', '#1.0.secret'],
  ])('takes the fragment off %s and hands it over without marking a read page', (page, fragment) => {
    const { replaced, session, take } = runGuard(`https://nylzo.test${page}${fragment}`);
    expect(replaced).toEqual([page]);
    expect(session.size).toBe(0);
    expect(take?.()).toBe(fragment);
  });

  it.each([
    ['/r/', '#2.0.AAAAAAAAAAAAAAAAAAAAAA.secret.hash'],
    ['/R', '#2.0.AAAAAAAAAAAAAAAAAAAAAA.secret.hash'],
    ['/', '#2.0.id.secret.hash'],
    ['/report', '#2.0.x'],
    ['/sent', `#${'x'.repeat(41)}`],
  ])('takes a link key off the mangled address %s without keeping it', (page, fragment) => {
    const { replaced, session, take } = runGuard(`https://nylzo.test${page}${fragment}`);
    expect(replaced).toEqual([page]);
    expect(session.size).toBe(0);
    expect(take).toBeUndefined();
  });

  it('keeps a fragment of 40 characters and takes off one of 41, as Protocol §4 counts them', () => {
    expect(runGuard(`https://nylzo.test/sent#${'x'.repeat(40)}`).replaced).toEqual([]);
    expect(runGuard(`https://nylzo.test/sent#${'x'.repeat(41)}`).replaced).toEqual(['/sent']);
  });

  it('also takes a link key off a fragment that changes later on the same page, but leaves read pages to reload', () => {
    const home = runGuard('https://nylzo.test/');
    home.changeHash('/#how-it-works');
    home.changeHash('/?x=1#2.0.id.secret.hash');
    expect(home.replaced).toEqual(['/?x=1']);
    const reader = runGuard('https://nylzo.test/r');
    reader.changeHash('/r#2.0.id.secret.hash');
    expect(reader.replaced).toEqual([]);
  });

  it('writes nothing to session storage on a local client origin (Protocol §14.1)', () => {
    const { replaced, session, take } = runGuard('http://abc.localhost:8095/r#2.0.id.secret.hash');
    expect(replaced).toEqual(['/r']);
    expect(session.size).toBe(0);
    expect(take?.()).toBe('#2.0.id.secret.hash');
  });

  it('leaves in-page anchors and read pages without a fragment alone', () => {
    for (const href of [
      'https://nylzo.test/#how',
      'https://nylzo.test/cs/#napsat',
      'https://nylzo.test/transparency#transparency-verify',
      'https://nylzo.test/r',
      'https://nylzo.test/m/AAAAAAAAAAAAAAAAAAAAAA',
    ]) {
      const { replaced, session, take } = runGuard(href);
      expect(replaced).toEqual([]);
      expect(session.size).toBe(0);
      expect(take).toBeUndefined();
    }
  });
});
