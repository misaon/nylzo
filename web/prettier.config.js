export default {
  singleQuote: true,
  plugins: ['prettier-plugin-svelte', 'prettier-plugin-tailwindcss'],
  tailwindStylesheet: './app/src/app.css',
  overrides: [{ files: '*.svelte', options: { parser: 'svelte' } }],
};
