export const STATUS_PAGES = ['error', 'maintenance'] as const;

export type StatusPage = (typeof STATUS_PAGES)[number];

export const MAINTENANCE_RELOAD_SECONDS = 30;
