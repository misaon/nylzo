import comments from '@eslint-community/eslint-plugin-eslint-comments/configs';
import javascript from '@eslint/js';
import perfectionist from 'eslint-plugin-perfectionist';
import regexp from 'eslint-plugin-regexp';
import unicorn from 'eslint-plugin-unicorn';
import { defineConfig } from 'eslint/config';
import globals from 'globals';
import typescript from 'typescript-eslint';

export default defineConfig(
  { ignores: ['**/node_modules/'] },
  javascript.configs.recommended,
  typescript.configs.strictTypeChecked,
  typescript.configs.stylisticTypeChecked,
  unicorn.configs.recommended,
  regexp.configs['flat/recommended'],
  comments.recommended,
  {
    languageOptions: {
      globals: globals.node,
      parserOptions: {
        projectService: { allowDefaultProject: ['*.js'] },
        tsconfigRootDir: import.meta.dirname,
      },
    },
    linterOptions: { reportUnusedDisableDirectives: 'error', reportUnusedInlineConfigs: 'error' },
    plugins: { perfectionist },
    rules: {
      curly: ['error', 'all'],
      eqeqeq: ['error', 'always'],
      'no-eval': 'error',
      'no-new-func': 'error',
      'object-shorthand': ['error', 'always'],
      'prefer-template': 'error',
      '@eslint-community/eslint-comments/require-description': ['error', { ignore: [] }],
      '@typescript-eslint/consistent-type-imports': ['error', { fixStyle: 'inline-type-imports' }],
      '@typescript-eslint/explicit-module-boundary-types': 'error',
      '@typescript-eslint/no-floating-promises': [
        'error',
        {
          allowForKnownSafeCalls: [
            { from: 'package', name: ['describe', 'it', 'suite', 'test'], package: 'node:test' },
          ],
        },
      ],
      '@typescript-eslint/no-confusing-void-expression': ['error', { ignoreArrowShorthand: true }],
      '@typescript-eslint/no-import-type-side-effects': 'error',
      '@typescript-eslint/restrict-template-expressions': ['error', { allowNumber: true }],
      '@typescript-eslint/switch-exhaustiveness-check': 'error',
      'perfectionist/sort-imports': ['error', { type: 'natural', newlinesBetween: 1 }],
      'perfectionist/sort-named-imports': ['error', { type: 'natural' }],
      'perfectionist/sort-named-exports': ['error', { type: 'natural' }],
      'perfectionist/sort-exports': ['error', { type: 'natural' }],
      'unicorn/consistent-boolean-name': 'off',
      'unicorn/filename-case': ['error', { cases: { kebabCase: true } }],
      'unicorn/no-null': 'off',
      'unicorn/no-process-exit': 'off',
    },
  },
  {
    files: ['source-dependency-gate/**', 'web-install/**'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              regex: String.raw`^(?!node:|\./[^/]+$|\.\./(source-dependency-gate|web-install)/[^/]+$)`,
              message:
                'The source dependency gate runs before automation dependencies are installed; use node: modules.',
            },
          ],
        },
      ],
    },
  },
  {
    files: ['**/*.js'],
    extends: [typescript.configs.disableTypeChecked],
  },
);
