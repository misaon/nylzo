import { createContext } from 'svelte';

import type { AdminTranslator } from '../messages/catalog.ts';
import type { DashboardFormat } from './format.ts';

export interface DashboardTools {
  readonly translate: AdminTranslator['translate'];
  readonly format: DashboardFormat;
}

export const [getDashboardTools, setDashboardTools] = createContext<DashboardTools>();
