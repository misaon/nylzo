import type { Language } from './language.ts';
import type { Plural } from './translator.svelte.ts';

const MINUTE = 60;
const HOUR = 3600;
const MEBIBYTE = 1024 * 1024;
const DEFAULT_WAIT_SECONDS = 60;

type DurationUnit = 'hour' | 'minute' | 'second';

function durationParts(seconds: number): readonly [value: number, unit: DurationUnit] {
  if (seconds < MINUTE) {
    return [Math.max(1, Math.ceil(seconds)), 'second'];
  }
  if (seconds < HOUR) {
    return [Math.ceil(seconds / MINUTE), 'minute'];
  }
  return [Math.ceil(seconds / HOUR), 'hour'];
}

const CZECH_HOURS_WITH_VE = new Set([2, 3, 4, 12, 13, 14, 20, 21, 22, 23]);

export function formatMoment(timestamp: number | string, language: Language): string {
  const moment = new Date(timestamp);
  if (language !== 'cs') {
    return new Intl.DateTimeFormat(language, {
      month: 'short',
      day: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    }).format(moment);
  }
  const date = new Intl.DateTimeFormat('cs', { day: 'numeric', month: 'numeric' }).format(moment);
  const time = new Intl.DateTimeFormat('cs', { hour: 'numeric', minute: '2-digit' }).format(moment);
  const preposition = CZECH_HOURS_WITH_VE.has(moment.getHours()) ? 've' : 'v';
  return `${date.replaceAll(' ', '\u{A0}')} ${preposition}\u{A0}${time}`;
}

export function formatDuration(seconds: number, language: Language): string {
  const [value, unit] = durationParts(seconds);
  return new Intl.NumberFormat(language, { style: 'unit', unit, unitDisplay: 'long' }).format(value);
}

export function formatWait(seconds: number | undefined, plural: Plural): string {
  const [value, unit] = durationParts(seconds ?? DEFAULT_WAIT_SECONDS);
  return plural(`wait.${unit}`, value);
}

export function formatClock(seconds: number): string {
  const whole = Math.max(0, Math.ceil(seconds));
  return [Math.floor(whole / MINUTE), whole % MINUTE].map((part) => String(part).padStart(2, '0')).join(':');
}

export function formatBytes(bytes: number, language: Language): string {
  const [value, unit] =
    bytes < MEBIBYTE ? [Math.max(1, Math.ceil(bytes / 1024)), 'kilobyte'] : [bytes / MEBIBYTE, 'megabyte'];
  return new Intl.NumberFormat(language, {
    style: 'unit',
    unit,
    unitDisplay: 'short',
    maximumFractionDigits: 1,
  }).format(value);
}

export function formatSize(bytes: number, language: Language): string {
  return bytes < 1024 ? `${new Intl.NumberFormat(language).format(bytes)}\u{A0}B` : formatBytes(bytes, language);
}
