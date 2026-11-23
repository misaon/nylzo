import { describe, expect, it } from 'vitest';

import type { Timeline } from '../charts/geometry/time.ts';
import { dashboardFormat } from './format.ts';

const MIDNIGHT = Date.UTC(2026, 10, 1) / 1000;
const KIBIBYTE = 1024;
const SPACE = '\u{A0}';

function timeline(resolutionSeconds: number, count: number): Timeline {
  return { from: MIDNIGHT, resolutionSeconds, count };
}

describe('dashboard format', () => {
  const english = dashboardFormat('en');

  it('formats numbers in the language of the dashboard', () => {
    const czech = dashboardFormat('cs');
    expect([english.count(1234.6), czech.count(1234.6)]).toEqual(['1,235', `1${SPACE}235`]);
    expect(english.compact(15_300)).toBe('15.3K');
    expect(english.percent(0.256)).toBe('26%');
    expect([english.decimal(0.5), czech.decimal(0.5)]).toEqual(['0.50', '0,50']);
    expect([english.change(0.12), english.change(-0.3), english.change(0)]).toEqual(['+12%', '-30%', '0%']);
  });

  it('shows sizes in binary units up to tebibytes and never below zero', () => {
    expect(english.bytes(-5)).toBe(`0${SPACE}B`);
    expect(english.bytes(1023)).toBe(`1,023${SPACE}B`);
    expect(english.bytes(1.5 * KIBIBYTE)).toBe(`1.5${SPACE}KiB`);
    expect(english.bytes(5 * KIBIBYTE ** 3)).toBe(`5${SPACE}GiB`);
    expect(english.bytes(3 * KIBIBYTE ** 5)).toBe(`3,072${SPACE}TiB`);
  });

  it('names countries and keeps the codes it cannot name', () => {
    expect(english.country('DE')).toBe('Germany');
    expect(dashboardFormat('cs').country('DE')).toBe('Německo');
    expect(english.country('XX')).toBe('XX');
    expect(english.country('not a region')).toBe('not a region');
  });

  it('labels ticks by hours, weekdays, days or months as the timeline grows', () => {
    expect(english.tick(timeline(3600, 24), 2)).toMatch(/^\d{1,2}:00\s[AP]M$/u);
    expect(english.tick(timeline(3600, 168), 2)).toMatch(/^[A-Z][a-z]{2} \d{1,2}$/u);
    expect(english.tick(timeline(86_400, 30), 2)).toBe('11/3');
    expect(english.tick(timeline(86_400, 365), 2)).toBe('Nov');
  });

  it('names a period by its hours, its day or its first and last day', () => {
    expect(english.period(timeline(3600, 24), 2)).toMatch(/^[A-Z][a-z]{2}, \d{1,2}\/\d{1,2} \d{1,2}:00[^–]*–[^–]*$/u);
    expect(english.period(timeline(86_400, 30), 2)).toBe('Tue, November 3, 2026');
    expect(english.period(timeline(86_400, 30), 2, 2)).toBe('Tue, November 3, 2026');
    expect(english.period(timeline(86_400, 30), 2, 5)).toBe('11/3 – 11/6');
  });

  it('formats moments with and without seconds and with the date', () => {
    expect(english.clock(MIDNIGHT)).toMatch(/^\d{1,2}:00\s[AP]M$/u);
    expect(english.time(MIDNIGHT)).toMatch(/^\d{1,2}:00:00\s[AP]M$/u);
    expect(english.moment(MIDNIGHT)).toMatch(/^(?:Oct 31|Nov 1|Nov 2), 2026/u);
  });

  it('describes a moment relative to now in the largest fitting unit', () => {
    const relative = new Intl.RelativeTimeFormat('en', { numeric: 'auto', style: 'short' });
    const now = MIDNIGHT;
    expect(english.relative(now, now)).toBe(relative.format(0, 'second'));
    expect(english.relative(now - 59, now)).toBe(relative.format(-59, 'second'));
    expect(english.relative(now - 5 * 60 - 20, now)).toBe(relative.format(-5, 'minute'));
    expect(english.relative(now - 2 * 3600 - 600, now)).toBe(relative.format(-2, 'hour'));
    expect(english.relative(now + 3 * 86_400, now)).toBe(relative.format(3, 'day'));
  });
});
