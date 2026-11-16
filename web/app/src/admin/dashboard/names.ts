import type { AdminTranslator } from '../messages/catalog.ts';
import type { DashboardFormat } from './format.ts';

const PAGES = ['home', 'sent', 'transparency', 'terms', 'agents', 'other'] as const;
const ROBOTS = ['search', 'ai', 'preview', 'monitor', 'audit', 'script', 'headless', 'flood', 'other'] as const;
const LIMITS = ['requests', 'create', 'read', 'read-bytes', 'admin'] as const;

export const UNKNOWN_COUNTRY = 'unknown';

function known<T extends string>(list: readonly T[], name: string): name is T {
  return list.some((entry) => entry === name);
}

export function pageName(translate: AdminTranslator['translate'], name: string): string {
  return known(PAGES, name) ? translate(`dashboard.page.${name}`) : name;
}

export function robotName(translate: AdminTranslator['translate'], name: string): string {
  return known(ROBOTS, name) ? translate(`dashboard.robot.${name}`) : name;
}

export function limitName(translate: AdminTranslator['translate'], name: string): string {
  return known(LIMITS, name) ? translate(`dashboard.limit.${name}`) : name;
}

export function countryName(translate: AdminTranslator['translate'], format: DashboardFormat, code: string): string {
  return code === UNKNOWN_COUNTRY || code === '' ? translate('dashboard.where.unknown') : format.country(code);
}
