import { createContext } from 'svelte';

import type { AdminTranslator } from './catalog.ts';

export const [getAdminTranslator, setAdminTranslator] = createContext<AdminTranslator>();
