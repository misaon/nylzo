import { readdirSync, readFileSync, statSync } from 'node:fs';

import { transform } from 'lightningcss';
import type { Declaration, TokenOrValue } from 'lightningcss';

import { expect, test } from './support/fixtures.ts';

const ASSETS = new URL('../../../internal/server/web/dist/assets/', import.meta.url);
// Owner decision of 7 Nov 2026: paint changes and collapsing heights may animate, layout offsets never.
const ANIMATABLE = new Set([
  'transform',
  'opacity',
  'translate',
  'scale',
  'rotate',
  'color',
  'background-color',
  'border-color',
  'outline-color',
  'outline-offset',
  'text-decoration-color',
  'fill',
  'stroke',
  'stroke-dasharray',
  'stroke-dashoffset',
  'box-shadow',
  'filter',
  'backdrop-filter',
  'clip-path',
  'mask-image',
  'mask-position',
  'mask-size',
  'mask-repeat',
  'visibility',
  'height',
  'block-size',
  'inline-size',
  'padding',
  'margin',
  'margin-top',
  'display',
  'overlay',
  'content-visibility',
  'pointer-events',
  'animation-timing-function',
]);
const COLOR_STOP = /^--(?:tw-gradient-(?:from|via|to)|edge-(?:from|via|to)|tile-(?:glow|top|bottom))$/u;
const NOBLE_SCRYPT_MARKER = 'maxmem';
// Gzip bytes the server sends: the size on 19 Nov 2026 plus 10 % (owner decision, #424).
const SIZE_BUDGET = { startup: 181_000, scripts: 525_000, styles: 32_000 } as const;

function builtAssets(suffix: string): string[] {
  return readdirSync(ASSETS).filter((name) => name.endsWith(suffix));
}

function readAsset(name: string): string {
  return readFileSync(new URL(name, ASSETS), 'utf8');
}

function declarationName(declaration: Declaration): string {
  if (declaration.property === 'unparsed') {
    return declaration.value.propertyId.property;
  }
  if (declaration.property === 'custom') {
    return declaration.value.name;
  }
  return declaration.property;
}

// Lightning CSS leaves a transition with allow-discrete unparsed, so each comma-separated part must start with its property.
function transitionedProperties(tokens: readonly TokenOrValue[]): string[] | undefined {
  const properties: string[] = [];
  let expecting = true;
  for (const token of tokens) {
    if (token.type === 'token' && token.value.type === 'comma') {
      expecting = true;
    } else if (expecting && !(token.type === 'token' && token.value.type === 'white-space')) {
      if (token.type !== 'token' || token.value.type !== 'ident') {
        return undefined;
      }
      properties.push(token.value.value);
      expecting = false;
    }
  }
  return properties;
}

function motionProblems(css: string): { readonly problems: string[]; readonly animated: boolean } {
  const problems: string[] = [];
  let animated = false;
  const animates = (where: string, property: string) => {
    if (property === 'none') {
      return;
    }
    animated = true;
    if (!ANIMATABLE.has(property) && !COLOR_STOP.test(property)) {
      problems.push(`${where} animates ${property}`);
    }
  };
  transform({
    filename: 'bundle.css',
    code: Buffer.from(css),
    visitor: {
      Rule: {
        keyframes(rule) {
          for (const keyframe of rule.value.keyframes) {
            const { declarations = [], importantDeclarations = [] } = keyframe.declarations;
            for (const declaration of [...declarations, ...importantDeclarations]) {
              animates('@keyframes', declarationName(declaration));
            }
          }
        },
      },
      Declaration(declaration) {
        if (declaration.property === 'transition-property') {
          for (const property of declaration.value) {
            animates('transition-property', property.property);
          }
        } else if (declaration.property === 'transition') {
          for (const transition of declaration.value) {
            animates('transition', transition.property.property);
          }
        } else if (
          declaration.property === 'unparsed' &&
          /^transition(?:-property)?$/u.test(declaration.value.propertyId.property)
        ) {
          const where = declaration.value.propertyId.property;
          const properties = transitionedProperties(declaration.value.value);
          if (properties === undefined) {
            problems.push(`${where} has a value the check cannot read`);
          }
          for (const property of properties ?? []) {
            animates(where, property);
          }
        }
      },
    },
  });
  return { problems, animated };
}

test('the motion check reads shorthands and keyframes', () => {
  expect(motionProblems('a { transition: opacity .2s, transform .2s }').problems).toEqual([]);
  expect(motionProblems('a { transition: width 1s }').problems).toEqual(['transition animates width']);
  expect(motionProblems('a { transition: opacity .3s, display .3s allow-discrete }').problems).toEqual([]);
  expect(motionProblems('a { transition: left .3s, display .3s allow-discrete }').problems).toEqual([
    'transition animates left',
  ]);
  expect(motionProblems('a { transition: all 1s }').problems).toEqual(['transition animates all']);
  expect(motionProblems('a { transition-property: width }').problems).toEqual(['transition-property animates width']);
  expect(motionProblems('a { transition: var(--t) }').problems).toEqual([
    'transition has a value the check cannot read',
  ]);
  expect(motionProblems('a { transition-property: var(--p) }').problems).toEqual([
    'transition-property has a value the check cannot read',
  ]);
  expect(motionProblems('a { transition-duration: var(--d); animation: var(--a) }').problems).toEqual([]);
  expect(motionProblems('@keyframes k { to { margin-left: 4px } }').problems).toEqual([
    '@keyframes animates margin-left',
  ]);
  expect(motionProblems('a { transition: none }')).toEqual({ problems: [], animated: false });
});

test('the built client animates no layout offset and evaluates no code', () => {
  const css = builtAssets('.css')
    .map((name) => readAsset(name))
    .join('\n');
  const { problems, animated } = motionProblems(css);
  expect(problems).toEqual([]);
  expect(animated).toBe(true);
  expect(css).toContain('prefers-reduced-motion');
  const scripts = builtAssets('.js')
    .map((name) => readAsset(name))
    .join('\n');
  expect(scripts).not.toMatch(/\bnew Function\b|\beval\(|svelte\/compiler/u);
  const index = readAsset('../index.html');
  const preloadedFonts = Array.from(
    index.matchAll(/<link rel="preload" href="\/assets\/([^"]+)" as="font" type="font\/woff2" crossorigin(?:="")?>/gu),
    (match) => match[1],
  );
  const fonts = builtAssets('.woff2');
  expect(preloadedFonts).toHaveLength(fonts.length);
  expect(new Set(preloadedFonts)).toEqual(new Set(fonts));
});

test('noble scrypt ships only in the key derivation worker (P5)', () => {
  const withScrypt = builtAssets('.js').filter((name) => readAsset(name).includes(NOBLE_SCRYPT_MARKER));
  expect(withScrypt).toEqual([expect.stringMatching(/^kdf-worker-[\w-]+\.js$/u)]);
});

function gzippedBytes(matches: (name: string) => boolean): number {
  return builtAssets('.gz')
    .filter((name) => matches(name.slice(0, -'.gz'.length)) && !name.startsWith('admin-'))
    .reduce((total, name) => total + statSync(new URL(name, ASSETS)).size, 0);
}

test('the public client stays within its compressed size budget', () => {
  const startup = gzippedBytes((name) => /^(?:index|start)-[\w-]+\.js$/u.test(name));
  const scripts = gzippedBytes((name) => name.endsWith('.js'));
  const styles = gzippedBytes((name) => name.endsWith('.css'));
  expect(startup, 'JavaScript every page loads').toBeGreaterThan(0);
  expect(startup, 'JavaScript every page loads').toBeLessThanOrEqual(SIZE_BUDGET.startup);
  expect(scripts, 'all JavaScript').toBeLessThanOrEqual(SIZE_BUDGET.scripts);
  expect(styles, 'CSS').toBeGreaterThan(0);
  expect(styles, 'CSS').toBeLessThanOrEqual(SIZE_BUDGET.styles);
});
