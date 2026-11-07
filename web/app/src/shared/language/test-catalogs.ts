import { czech } from './czech.ts';
import { english } from './english.ts';
import { addCatalog } from './translator.svelte.ts';

addCatalog('cs', czech);
addCatalog('en', english);
