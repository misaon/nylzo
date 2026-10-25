import { type Declaration, transform } from 'lightningcss';
import { readdirSync, readFileSync } from 'node:fs';
import { gzipSync } from 'node:zlib';

import { expect, test } from './support/fixtures.ts';

const ASSETS = new URL('../../../internal/server/web/dist/assets/', import.meta.url);
const ANIMATABLE = new Set(['transform', 'opacity', 'translate', 'scale', 'rotate']);
const NOBLE_SCRYPT_MARKER = 'maxmem';

function builtAssets(suffix: string): string[] {
  return readdirSync(ASSETS).filter((name) => name.endsWith(suffix));
}

function readAsset(name: string): string {
  return readFileSync(new URL(name, ASSETS), 'utf8');
}

function gzippedSize(suffix: string): number {
  return builtAssets(suffix).reduce((sum, name) => sum + gzipSync(readFileSync(new URL(name, ASSETS))).length, 0);
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

function motionProblems(css: string): { readonly problems: string[]; readonly animated: boolean } {
  const problems: string[] = [];
  let animated = false;
  const animates = (where: string, property: string) => {
    if (property === 'none') {
      return;
    }
    animated = true;
    if (!ANIMATABLE.has(property)) {
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
          /^transition(?:-property)?$/.test(declaration.value.propertyId.property)
        ) {
          problems.push(`${declaration.value.propertyId.property} has a value the check cannot read`);
        }
      },
    },
  });
  return { problems, animated };
}

test('the motion check reads shorthands and keyframes (AK19)', () => {
  expect(motionProblems('a { transition: opacity .2s, transform .2s }').problems).toEqual([]);
  expect(motionProblems('a { transition: width 1s }').problems).toEqual(['transition animates width']);
  expect(motionProblems('a { transition: all 1s }').problems).toEqual(['transition animates all']);
  expect(motionProblems('a { transition-property: height }').problems).toEqual(['transition-property animates height']);
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

test('the built client stays within its budget, animates only transform and opacity and evaluates no code (AK19, AK20)', () => {
  expect(gzippedSize('.js')).toBeLessThanOrEqual(150 * 1024);
  expect(gzippedSize('.css')).toBeLessThanOrEqual(40 * 1024);
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
  expect(scripts).not.toMatch(/\bnew Function\b|\beval\(|svelte\/compiler/);
  const index = readAsset('../index.html');
  const preloadedFonts = Array.from(
    index.matchAll(/<link rel="preload" href="\/assets\/([^"]+)" as="font" type="font\/woff2" crossorigin>/g),
    (match) => match[1],
  );
  const fonts = builtAssets('.woff2');
  expect(preloadedFonts).toHaveLength(fonts.length);
  expect(new Set(preloadedFonts)).toEqual(new Set(fonts));
});

test('noble scrypt ships only in the key derivation worker (P5)', () => {
  const withScrypt = builtAssets('.js').filter((name) => readAsset(name).includes(NOBLE_SCRYPT_MARKER));
  expect(withScrypt).toEqual([expect.stringMatching(/^kdf-worker-[\w-]+\.js$/)]);
});
