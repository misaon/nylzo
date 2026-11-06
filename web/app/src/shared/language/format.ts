import type { Language } from './language.ts';

const MINUTE = 60;
const HOUR = 3600;
const MEBIBYTE = 1024 * 1024;
const DEFAULT_WAIT_SECONDS = 60;

function durationParts(seconds: number): readonly [value: number, unit: string] {
  if (seconds < MINUTE) {
    return [Math.max(1, Math.ceil(seconds)), 'second'];
  }
  if (seconds < HOUR) {
    return [Math.ceil(seconds / MINUTE), 'minute'];
  }
  return [Math.ceil(seconds / HOUR), 'hour'];
}

const CZECH_HOURS_WITH_VE = new Set([2, 3, 4, 12, 13, 14, 20, 21, 22, 23]);

export function formatMoment(timestamp: string, language: Language): string {
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

export function formatWait(seconds: number | undefined, language: Language): string {
  const duration = formatDuration(seconds ?? DEFAULT_WAIT_SECONDS, language);
  return language === 'cs' ? duration.replace(/^1(\s)(minut|hodin|sekund)a$/u, '1$1$2u') : duration;
}

type PluralForms = Readonly<Record<Intl.LDMLPluralRule, string>>;

function pluralForms(one: string, few: string, many: string, other: string): PluralForms {
  return { zero: other, one, two: few, few, many, other };
}

const UNIT_FORMS = {
  day: { cs: pluralForms('den', 'dny', 'dne', 'dní'), en: pluralForms('day', 'days', 'days', 'days') },
  attempt: {
    cs: pluralForms('pokus', 'pokusy', 'pokusu', 'pokusů'),
    en: pluralForms('attempt', 'attempts', 'attempts', 'attempts'),
  },
  character: {
    cs: pluralForms('znak', 'znaky', 'znaku', 'znaků'),
    en: pluralForms('character', 'characters', 'characters', 'characters'),
  },
  file: {
    cs: pluralForms('soubor', 'soubory', 'souboru', 'souborů'),
    en: pluralForms('file', 'files', 'files', 'files'),
  },
} as const satisfies Readonly<Record<string, Readonly<Record<Language, PluralForms>>>>;

export function formatCount(count: number, unit: keyof typeof UNIT_FORMS, language: Language): string {
  const form = UNIT_FORMS[unit][language][new Intl.PluralRules(language).select(count)];
  return `${new Intl.NumberFormat(language).format(count)}\u{A0}${form}`;
}

export function formatDays(days: number, language: Language): string {
  return formatCount(days, 'day', language);
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
