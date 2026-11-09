import { loadCatalog } from './catalog.ts';
import { LANGUAGES } from './language.ts';

await Promise.all(LANGUAGES.map(async (language) => loadCatalog(language)));
