import type { Language } from './i18n.svelte.ts';

const MINUTE = 60;
const HOUR = 3600;
const MEBIBYTE = 1024 * 1024;

export function formatTime(iso: string, language: Language): string {
  return new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso));
}

export function formatDuration(seconds: number, language: Language): string {
  const [value, unit] =
    seconds < MINUTE ? [Math.max(1, Math.ceil(seconds)), 'second'] : seconds < HOUR ? [Math.ceil(seconds / MINUTE), 'minute'] : [Math.ceil(seconds / HOUR), 'hour'];
  return new Intl.NumberFormat(language, { style: 'unit', unit, unitDisplay: 'long' }).format(value);
}

export function formatBytes(bytes: number, language: Language): string {
  const [value, unit] = bytes < MEBIBYTE ? [Math.max(1, Math.ceil(bytes / 1024)), 'kilobyte'] : [bytes / MEBIBYTE, 'megabyte'];
  return new Intl.NumberFormat(language, { style: 'unit', unit, unitDisplay: 'short', maximumFractionDigits: 1 }).format(value);
}
