import { parse as parseScript } from 'acorn';
import { transform } from 'lightningcss';
import { createHash } from 'node:crypto';
import path from 'node:path';
import { type DefaultTreeAdapterMap, parse, serialize } from 'parse5';

type Assets = ReadonlyMap<string, Uint8Array>;
type HtmlNode = DefaultTreeAdapterMap['node'];

// This policy intersects with the server's nonce policy. Inline CSS still
// needs the server nonce; the second policy only adds the origin restriction.
const NONCE = '__NYLZO_CSP_NONCE__';

function assetAt(url: string, from: string, assets: Assets): Uint8Array {
  if (!/^(?:\/assets\/|\.\.?\/)/.test(url) || /[?#%\\]/.test(url)) {
    throw new Error(`resource integrity: nonlocal resource ${url} in ${from}`);
  }
  const resolved = path.posix.resolve(path.posix.dirname(from), url);
  const data = assets.get(resolved);
  if (!data || !resolved.startsWith('/assets/')) {
    throw new Error(`resource integrity: missing bundled resource ${url} in ${from}`);
  }
  return data;
}

export function integrity(data: Uint8Array): string {
  return `sha384-${createHash('sha384').update(data).digest('base64')}`;
}

function visitScript(value: unknown, visit: (node: Record<string, unknown>) => void): void {
  if (!value || typeof value !== 'object') {
    return;
  }
  if (Array.isArray(value)) {
    for (const child of value) {
      visitScript(child, visit);
    }
    return;
  }
  const node = value as Record<string, unknown>;
  if (typeof node['type'] === 'string') {
    visit(node);
  }
  for (const child of Object.values(node)) {
    visitScript(child, visit);
  }
}

export function checkScript(code: string, from: string, assets: Assets): void {
  visitScript(parseScript(code, { ecmaVersion: 'latest', sourceType: 'module' }), (node) => {
    if (
      !['ImportDeclaration', 'ImportExpression', 'ExportAllDeclaration', 'ExportNamedDeclaration'].includes(
        String(node['type']),
      )
    ) {
      return;
    }
    const source = node['source'] as
      | { type?: string; value?: unknown; expressions?: unknown[]; quasis?: { value: { cooked: string | null } }[] }
      | undefined;
    if (!source) {
      return;
    }
    let url: unknown;
    if (source.type === 'Literal') {
      url = source.value;
    } else if (source.type === 'TemplateLiteral' && source.expressions?.length === 0) {
      url = source.quasis?.[0]?.value.cooked;
    }
    if (typeof url !== 'string') {
      throw new TypeError(`resource integrity: computed module import in ${from}`);
    }
    assetAt(url, from, assets);
  });
}

export function checkStyles(code: string, from: string, assets: Assets): void {
  const result = transform({ filename: from, code: Buffer.from(code), analyzeDependencies: true });
  const dependencies = result.dependencies ?? [];
  for (const dependency of dependencies) {
    if (dependency.type === 'import') {
      throw new Error(`resource integrity: unbundled CSS import in ${from}`);
    }
    if (dependency.type === 'url') {
      assetAt(dependency.url, from, assets);
    }
  }
}

function visitHtml(node: HtmlNode, visit: (element: DefaultTreeAdapterMap['element']) => void): void {
  if ('tagName' in node) {
    visit(node);
    if (node.tagName === 'template' && 'content' in node) {
      visitHtml(node.content, visit);
    }
  }
  if ('childNodes' in node) {
    for (const child of node.childNodes) {
      visitHtml(child, visit);
    }
  }
}

export function secureHtml(html: string, assets: Assets): string {
  const document = parse(html, { scriptingEnabled: false });
  visitHtml(document, (element) => {
    const attributes = new Map(element.attrs.map(({ name, value }) => [name, value]));
    const script = element.tagName === 'script';
    const link = element.tagName === 'link';
    const relationships = attributes.get('rel')?.toLowerCase().split(/\s+/) ?? [];
    const resource = attributes.get(script ? 'src' : 'href');
    const executableLink =
      link &&
      (relationships.includes('stylesheet') ||
        relationships.includes('modulepreload') ||
        (relationships.includes('preload') && ['script', 'style'].includes(attributes.get('as') ?? '')));
    if (script && !resource && attributes.get('type') !== 'application/ld+json') {
      throw new Error('resource integrity: executable inline scripts and import maps are forbidden');
    }
    if (script || executableLink) {
      if (resource) {
        const data = assetAt(resource, '/index.html', assets);
        element.attrs = element.attrs.filter(({ name }) => name !== 'integrity' && name !== 'crossorigin');
        element.attrs.push({ name: 'integrity', value: integrity(data) }, { name: 'crossorigin', value: 'anonymous' });
      } else if (executableLink) {
        throw new Error('resource integrity: stylesheet or script preload has no URL');
      }
    } else if (element.tagName === 'style') {
      const css = element.childNodes
        .filter((child) => child.nodeName === '#text')
        .map((child) => (child as DefaultTreeAdapterMap['textNode']).value)
        .join('');
      checkStyles(css, '/index.html', assets);
    }
  });
  const root = document.childNodes.find((node) => node.nodeName === 'html');
  const head = root && 'childNodes' in root ? root.childNodes.find((node) => node.nodeName === 'head') : undefined;
  if (!head || !('tagName' in head)) {
    throw new Error('resource integrity: missing document head');
  }
  const scripts = Array.from(assets, ([name, data]) => ({ name, hash: integrity(data) }))
    .filter(({ name }) => name.endsWith('.js'))
    .toSorted((first, second) => first.name.localeCompare(second.name));
  const metadata = Object.fromEntries(scripts.map(({ name, hash }) => [name, hash]));
  const contents = JSON.stringify({ integrity: metadata }).replaceAll('<', String.raw`\u003c`);
  const importMap: DefaultTreeAdapterMap['element'] = {
    nodeName: 'script',
    tagName: 'script',
    namespaceURI: head.namespaceURI,
    attrs: [
      { name: 'type', value: 'importmap' },
      { name: 'nonce', value: NONCE },
    ],
    childNodes: [],
    parentNode: head,
  };
  importMap.childNodes.push({ nodeName: '#text', value: contents, parentNode: importMap });
  head.childNodes.unshift(
    {
      nodeName: 'meta',
      tagName: 'meta',
      namespaceURI: head.namespaceURI,
      attrs: [
        { name: 'http-equiv', value: 'Content-Security-Policy' },
        {
          name: 'content',
          value: `script-src 'self' '${integrity(Buffer.from(contents))}'; style-src 'self' 'unsafe-inline'`,
        },
      ],
      childNodes: [],
      parentNode: head,
    },
    importMap,
  );
  return serialize(document, { scriptingEnabled: false });
}
