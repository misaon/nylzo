import { LANGUAGES } from './language.ts';
import { loadCatalog } from './translator.svelte.ts';

await Promise.all(LANGUAGES.map(async (language) => loadCatalog(language)));
