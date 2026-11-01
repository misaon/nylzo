import { createContext } from 'svelte';

import type { LinkProblem } from './link-handoff.ts';

export type OpenLink = (link: string) => LinkProblem | undefined;

export const [getLinkOpener, setLinkOpener] = createContext<OpenLink>();
