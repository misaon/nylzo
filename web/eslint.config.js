import comments from '@eslint-community/eslint-plugin-eslint-comments/configs';
import javascript from '@eslint/js';
import vitest from '@vitest/eslint-plugin';
import perfectionist from 'eslint-plugin-perfectionist';
import playwright from 'eslint-plugin-playwright';
import regexp from 'eslint-plugin-regexp';
import svelte from 'eslint-plugin-svelte';
import unicorn from 'eslint-plugin-unicorn';
import { defineConfig } from 'eslint/config';
import globals from 'globals';
import typescript from 'typescript-eslint';

import svelteConfig from './app/svelte.config.js';

const markupSinks = ['innerHTML', 'outerHTML', 'insertAdjacentHTML', 'setHTMLUnsafe', 'parseHTMLUnsafe'].map(
  (property) => ({
    property,
    message: 'Markup sinks parse HTML; build the DOM with Svelte instead.',
  }),
);

export default defineConfig(
  {
    ignores: ['**/node_modules/', '**/coverage/', 'app/.prerender/', 'e2e/test-results/', 'e2e/.bin/'],
  },
  javascript.configs.recommended,
  typescript.configs.strictTypeChecked,
  typescript.configs.stylisticTypeChecked,
  svelte.configs.recommended,
  svelte.configs.prettier,
  unicorn.configs.recommended,
  regexp.configs['flat/recommended'],
  comments.recommended,
  {
    languageOptions: {
      globals: globals.browser,
      parserOptions: {
        projectService: { allowDefaultProject: ['*.js'] },
        tsconfigRootDir: import.meta.dirname,
        extraFileExtensions: ['.svelte'],
      },
    },
    linterOptions: { reportUnusedDisableDirectives: 'error', reportUnusedInlineConfigs: 'error' },
    plugins: { perfectionist },
    rules: {
      curly: ['error', 'all'],
      eqeqeq: ['error', 'always'],
      'no-console': 'error',
      'no-eval': 'error',
      'no-new-func': 'error',
      'no-restricted-properties': [
        'error',
        ...markupSinks,
        { object: 'document', property: 'write', message: 'document.write parses HTML.' },
        { object: 'document', property: 'writeln', message: 'document.writeln parses HTML.' },
      ],
      'object-shorthand': ['error', 'always'],
      'prefer-template': 'error',
      '@eslint-community/eslint-comments/require-description': ['error', { ignore: [] }],
      '@typescript-eslint/consistent-type-imports': ['error', { fixStyle: 'inline-type-imports' }],
      '@typescript-eslint/explicit-module-boundary-types': 'error',
      '@typescript-eslint/no-confusing-void-expression': ['error', { ignoreArrowShorthand: true }],
      '@typescript-eslint/no-import-type-side-effects': 'error',
      '@typescript-eslint/restrict-template-expressions': ['error', { allowNumber: true }],
      '@typescript-eslint/switch-exhaustiveness-check': 'error',
      'perfectionist/sort-imports': ['error', { type: 'natural', newlinesBetween: 1 }],
      'perfectionist/sort-named-imports': ['error', { type: 'natural' }],
      'perfectionist/sort-named-exports': ['error', { type: 'natural' }],
      'perfectionist/sort-exports': ['error', { type: 'natural' }],
      'unicorn/consistent-boolean-name': 'off',
      'unicorn/filename-case': ['error', { cases: { kebabCase: true, pascalCase: true } }],
      'unicorn/name-replacements': ['error', { allowList: { Props: true, props: true } }],
      'unicorn/no-null': 'off',
      'unicorn/number-literal-case': ['error', { hexadecimalValue: 'lowercase' }],
    },
  },
  {
    files: ['**/*.svelte', '**/*.svelte.ts'],
    languageOptions: { parserOptions: { parser: typescript.parser, svelteConfig } },
    rules: {
      'svelte/block-lang': ['error', { script: 'ts' }],
      'svelte/button-has-type': 'error',
      'svelte/consistent-selector-style': 'error',
      'svelte/no-add-event-listener': 'error',
      'svelte/no-at-html-tags': 'error',
      'svelte/no-at-const-tags': 'error',
      'svelte/no-inline-styles': ['error', { allowTransitions: false }],
      'svelte/no-target-blank': 'error',
      'svelte/no-top-level-browser-globals': 'error',
      'svelte/prefer-const': 'error',
      'svelte/prefer-derived-over-derived-by': 'error',
      'svelte/require-event-prefix': 'error',
      'unicorn/no-top-level-assignment-in-function': 'off',
    },
  },
  {
    files: ['**/*.js'],
    extends: [typescript.configs.disableTypeChecked],
  },
  {
    files: ['**/*.test.ts'],
    extends: [vitest.configs.recommended],
  },
  {
    files: ['e2e/**/*.ts'],
    extends: [playwright.configs['flat/recommended']],
    languageOptions: { globals: globals.node },
  },
  {
    files: ['*.js', 'app/vite.config.ts', 'app/scripts/**', 'e2e/serve.ts', 'e2e/playwright.config.ts'],
    languageOptions: { globals: globals.node },
    rules: { 'no-console': 'off' },
  },
);
