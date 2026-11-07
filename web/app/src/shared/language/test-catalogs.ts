import { loadCatalog } from './catalogs.ts';
import { LANGUAGES } from './language.ts';

await Promise.all(LANGUAGES.map(async (language) => loadCatalog(language)));
