import type { Language } from './i18n.svelte.ts';

const MINUTE = 60;
const HOUR = 3600;
const MEBIBYTE = 1024 * 1024;
export const DEFAULT_WAIT_SECONDS = 60;

export function formatTime(iso: string, language: Language): string {
  return new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso));
}

export function formatDuration(seconds: number, language: Language): string {
  const [value, unit] =
    seconds < MINUTE ? [Math.max(1, Math.ceil(seconds)), 'second'] : seconds < HOUR ? [Math.ceil(seconds / MINUTE), 'minute'] : [Math.ceil(seconds / HOUR), 'hour'];
  return new Intl.NumberFormat(language, { style: 'unit', unit, unitDisplay: 'long' }).format(value);
}

export function formatWait(seconds: number | undefined, language: Language): string {
  const duration = formatDuration(seconds ?? DEFAULT_WAIT_SECONDS, language);
  return language === 'cs' ? duration.replace(/^1(\s)(minut|hodin|sekund)a$/u, '1$1$2u') : duration;
}

export function formatDays(days: number, language: Language): string {
  return new Intl.NumberFormat(language, { style: 'unit', unit: 'day', unitDisplay: 'long' }).format(days);
}

export function formatBytes(bytes: number, language: Language): string {
  const [value, unit] = bytes < MEBIBYTE ? [Math.max(1, Math.ceil(bytes / 1024)), 'kilobyte'] : [bytes / MEBIBYTE, 'megabyte'];
  return new Intl.NumberFormat(language, { style: 'unit', unit, unitDisplay: 'short', maximumFractionDigits: 1 }).format(value);
}
