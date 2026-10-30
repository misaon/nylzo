import { parse } from 'parse5';
import { describe, expect, it } from 'vitest';

import { checkScript, checkStyles, integrity, secureHtml } from '../../scripts/resource-integrity.ts';

const assets = new Map([
  ['/assets/app.js', Buffer.from('import("./start.js")')],
  ['/assets/start.js', Buffer.from('export const start = () => {}')],
  ['/assets/style.css', Buffer.from('body{color:red}')],
  ['/assets/font.woff2', Buffer.from('font')],
]);
const nonce = '__NYLZO_CSP_NONCE__';
const html = `<html><head><script nonce="${nonce}" src="/assets/app.js" type="module"></script><link nonce="${nonce}" rel="stylesheet" href="/assets/style.css"></head><body></body></html>`;

describe('final frontend resource policy', () => {
  it('seals scripts, styles and module preloads after prerendering without changing their nonces', () => {
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
    expect(output.match(new RegExp(`nonce="${nonce}"`, 'g'))).toHaveLength(3);
    expect(output).toContain('<head><meta http-equiv="Content-Security-Policy"');
    expect(output).toContain("style-src 'self' 'unsafe-inline'");
    expect(output).toContain('type="importmap"');
  });

  it.each([
    'https://evil.invalid/a.js',
    '//evil.invalid/a.js',
    '/assets/../a.js',
    '/assets/app.js?new=1',
    'data:text/javascript,alert(1)',
    'https&#58;//evil.invalid/a.js',
  ])('rejects unbundled HTML script URL %s even with a nonce', (url) => {
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
        html.replace('</head>', () => `<script nonce="${nonce}" type="application/ld+json">{}</script></head>`),
        assets,
      ),
    ).toContain('application/ld+json');
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
          html.replace('</head>', () => `<style nonce="${nonce}">${css}</style></head>`),
          assets,
        ),
      ).toThrow(/resource integrity/);
    }
  });
});
