import type { Language } from './language.ts';

interface FlagShape {
  readonly path: string;
  readonly fill?: string;
  readonly stroke?: string;
  readonly strokeWidth?: number;
}

const FIELD = 'M0 0H30V20H0Z';
const TOP_HALF = 'M0 0H30V10H0Z';
const BOTTOM_HALF = 'M0 10H30V20H0Z';
const TOP_THIRD = 'M0 0H30V6.67H0Z';
const BOTTOM_THIRD = 'M0 13.33H30V20H0Z';
const LEFT_THIRD = 'M0 0H10V20H0Z';
const RIGHT_THIRD = 'M20 0H30V20H20Z';
const UNION_DIAGONALS = 'M0 0L30 20M30 0L0 20';
const UNION_CROSS = 'M15 0V20M0 10H30';

export const FLAGS: Readonly<Record<Language, readonly FlagShape[]>> = {
  cs: [
    { path: TOP_HALF, fill: '#fff' },
    { path: BOTTOM_HALF, fill: '#D7141A' },
    { path: 'M0 0L15 10L0 20Z', fill: '#11457E' },
  ],
  de: [
    { path: TOP_THIRD, fill: '#000' },
    { path: 'M0 6.67H30V13.33H0Z', fill: '#DD0000' },
    { path: BOTTOM_THIRD, fill: '#FFCE00' },
  ],
  en: [
    { path: FIELD, fill: '#012169' },
    { path: UNION_DIAGONALS, stroke: '#fff', strokeWidth: 4 },
    { path: UNION_DIAGONALS, stroke: '#C8102E', strokeWidth: 1.6 },
    { path: UNION_CROSS, stroke: '#fff', strokeWidth: 6 },
    { path: UNION_CROSS, stroke: '#C8102E', strokeWidth: 3.6 },
  ],
  es: [
    { path: FIELD, fill: '#AA151B' },
    { path: 'M0 5H30V15H0Z', fill: '#F1BF00' },
  ],
  fr: [
    { path: FIELD, fill: '#fff' },
    { path: LEFT_THIRD, fill: '#002654' },
    { path: RIGHT_THIRD, fill: '#CE1126' },
  ],
  id: [
    { path: TOP_HALF, fill: '#CE1126' },
    { path: BOTTOM_HALF, fill: '#fff' },
  ],
  it: [
    { path: FIELD, fill: '#fff' },
    { path: LEFT_THIRD, fill: '#009246' },
    { path: RIGHT_THIRD, fill: '#CE2B37' },
  ],
  ja: [
    { path: FIELD, fill: '#fff' },
    { path: 'M9 10a6 6 0 1 0 12 0a6 6 0 1 0-12 0Z', fill: '#BC002D' },
  ],
  pl: [
    { path: TOP_HALF, fill: '#fff' },
    { path: BOTTOM_HALF, fill: '#DC143C' },
  ],
  'pt-BR': [
    { path: FIELD, fill: '#009C3B' },
    { path: 'M15 2.2L27.4 10L15 17.8L2.6 10Z', fill: '#FFDF00' },
    { path: 'M10.4 10a4.6 4.6 0 1 0 9.2 0a4.6 4.6 0 1 0-9.2 0Z', fill: '#002776' },
    { path: 'M10.6 8.9Q15 7.9 19.5 11.1', stroke: '#fff', strokeWidth: 0.9 },
  ],
  sk: [
    { path: TOP_THIRD, fill: '#fff' },
    { path: 'M0 6.67H30V13.33H0Z', fill: '#0B4EA2' },
    { path: BOTTOM_THIRD, fill: '#EE1C25' },
    { path: 'M6.6 4.4H14.4V11.4C14.4 14 10.5 15.8 10.5 15.8S6.6 14 6.6 11.4Z', fill: '#EE1C25', stroke: '#fff' },
    { path: 'M10.5 6V13M8.9 7.7H12.1M8.3 9.8H12.7', stroke: '#fff', strokeWidth: 1 },
    { path: 'M7.4 13.4Q9 11.9 10.5 13.1Q12 11.9 13.6 13.4L10.5 15.4Z', fill: '#0B4EA2' },
  ],
  'zh-Hans': [
    { path: FIELD, fill: '#EE1C25' },
    {
      path: 'M5 2L5.67 4.07L7.85 4.07L6.09 5.35L6.76 7.43L5 6.15L3.24 7.43L3.91 5.35L2.15 4.07L4.33 4.07Z',
      fill: '#FFFF00',
    },
    {
      path: 'M9.14 2.51L9.62 1.97L9.25 1.34L9.91 1.63L10.39 1.08L10.33 1.8L11 2.09L10.29 2.25L10.22 2.97L9.85 2.35Z',
      fill: '#FFFF00',
    },
    {
      path: 'M11.01 4.14L11.66 3.82L11.56 3.1L12.07 3.62L12.72 3.3L12.38 3.95L12.88 4.47L12.17 4.34L11.83 4.99L11.73 4.27Z',
      fill: '#FFFF00',
    },
    {
      path: 'M11.04 6.73L11.76 6.7L11.96 6L12.21 6.68L12.94 6.66L12.37 7.1L12.62 7.79L12.01 7.38L11.44 7.83L11.64 7.13Z',
      fill: '#FFFF00',
    },
    {
      path: 'M9.22 8.38L9.9 8.63L10.35 8.06L10.32 8.79L11 9.05L10.3 9.24L10.26 9.96L9.87 9.36L9.16 9.55L9.62 8.98Z',
      fill: '#FFFF00',
    },
  ],
};
