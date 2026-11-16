import { LONG_HOURLY, periodStart, SECONDS_PER_DAY } from '../charts/geometry/time.ts';
import type { Timeline } from '../charts/geometry/time.ts';
import type { AdminLanguage } from '../messages/catalog.ts';

const BINARY_UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'] as const;
const KIBIBYTE = 1024;
const MINUTE = 60;
const HOUR = 3600;

function date(seconds: number): Date {
  return new Date(seconds * 1000);
}

export interface DashboardFormat {
  readonly count: (value: number) => string;
  readonly compact: (value: number) => string;
  readonly percent: (fraction: number) => string;
  readonly decimal: (value: number) => string;
  readonly change: (fraction: number) => string;
  readonly bytes: (bytes: number) => string;
  readonly country: (code: string) => string;
  readonly tick: (timeline: Timeline, index: number) => string;
  readonly period: (timeline: Timeline, first: number, last?: number) => string;
  readonly clock: (seconds: number) => string;
  readonly time: (seconds: number) => string;
  readonly moment: (seconds: number) => string;
  readonly relative: (seconds: number, now: number) => string;
}

function binary(bytes: number, number: Intl.NumberFormat): string {
  let value = Math.max(0, bytes);
  let unit = 0;
  while (value >= KIBIBYTE && unit < BINARY_UNITS.length - 1) {
    value /= KIBIBYTE;
    unit += 1;
  }
  return `${number.format(value)}\u{A0}${BINARY_UNITS[unit] ?? 'B'}`;
}

function relative(difference: number, format: Intl.RelativeTimeFormat): string {
  const magnitude = Math.abs(difference);
  if (magnitude < MINUTE) {
    return format.format(Math.round(difference), 'second');
  }
  if (magnitude < HOUR) {
    return format.format(Math.round(difference / MINUTE), 'minute');
  }
  if (magnitude < SECONDS_PER_DAY) {
    return format.format(Math.round(difference / HOUR), 'hour');
  }
  return format.format(Math.round(difference / SECONDS_PER_DAY), 'day');
}

export function dashboardFormat(language: AdminLanguage): DashboardFormat {
  const whole = new Intl.NumberFormat(language, { maximumFractionDigits: 0 });
  const short = new Intl.NumberFormat(language, { notation: 'compact', maximumFractionDigits: 1 });
  const percent = new Intl.NumberFormat(language, { style: 'percent', maximumFractionDigits: 0 });
  const change = new Intl.NumberFormat(language, {
    style: 'percent',
    maximumFractionDigits: 0,
    signDisplay: 'exceptZero',
  });
  const decimal = new Intl.NumberFormat(language, { maximumFractionDigits: 1 });
  const precise = new Intl.NumberFormat(language, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  const regions = new Intl.DisplayNames([language], { type: 'region', fallback: 'code' });
  const hour = new Intl.DateTimeFormat(language, { hour: 'numeric', minute: '2-digit' });
  const weekday = new Intl.DateTimeFormat(language, { weekday: 'short', day: 'numeric' });
  const day = new Intl.DateTimeFormat(language, { day: 'numeric', month: 'numeric', timeZone: 'UTC' });
  const month = new Intl.DateTimeFormat(language, { month: 'short', timeZone: 'UTC' });
  const longDay = new Intl.DateTimeFormat(language, {
    weekday: 'short',
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    timeZone: 'UTC',
  });
  const withSeconds = new Intl.DateTimeFormat(language, { hour: 'numeric', minute: '2-digit', second: '2-digit' });
  const hourDay = new Intl.DateTimeFormat(language, { weekday: 'short', day: 'numeric', month: 'numeric' });
  const moment = new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' });
  const ago = new Intl.RelativeTimeFormat(language, { numeric: 'auto', style: 'short' });

  return {
    count: (value) => whole.format(value),
    compact: (value) => short.format(value),
    percent: (fraction) => percent.format(fraction),
    decimal: (value) => precise.format(value),
    change: (fraction) => change.format(fraction),
    bytes: (bytes) => binary(bytes, decimal),
    country: (code) => {
      try {
        return regions.of(code) ?? code;
      } catch {
        return code;
      }
    },
    tick: (timeline, index) => {
      const start = date(periodStart(timeline, index));
      if (timeline.resolutionSeconds < SECONDS_PER_DAY) {
        return timeline.count > LONG_HOURLY ? weekday.format(start) : hour.format(start);
      }
      return timeline.count > 120 ? month.format(start) : day.format(start);
    },
    period: (timeline, first, last = first) => {
      const start = date(periodStart(timeline, first));
      const end = date(periodStart(timeline, last + 1));
      if (timeline.resolutionSeconds < SECONDS_PER_DAY) {
        return `${hourDay.format(start)} ${hour.formatRange(start, end)}`;
      }
      return first === last
        ? longDay.format(start)
        : `${day.format(start)} – ${day.format(date(periodStart(timeline, last)))}`;
    },
    clock: (value) => hour.format(date(value)),
    time: (value) => withSeconds.format(date(value)),
    moment: (seconds) => moment.format(date(seconds)),
    relative: (seconds, now) => relative(seconds - now, ago),
  };
}
