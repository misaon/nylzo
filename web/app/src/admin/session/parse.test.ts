import { describe, expect, it } from 'vitest';

import { ResponseError } from '../../shared/api/api-errors.ts';
import { TIMELINE_METRICS } from '../api/types.ts';
import { parseLive, parseOverview } from './parse.ts';

const METRICS = Object.fromEntries(TIMELINE_METRICS.map((metric, index) => [metric, index]));

function overviewJson(changes: Record<string, unknown> = {}) {
  return {
    challenge: 'ignored',
    range: '24h',
    from: 1_700_000_000,
    resolution_s: 3600,
    series: Object.fromEntries(TIMELINE_METRICS.map((metric) => [metric, [1, 2]])),
    totals: METRICS,
    previous: METRICS,
    countries: [{ name: 'CZ', value: 3 }],
    pages: null,
    robots: [],
    stored: 4,
    storage: { free_bytes: 10, total_bytes: 20, reserve_bytes: 5, database_bytes: 2 },
    security: { passkeys: 1, last_sign_in: 0, session_expires_at: 1_700_000_900 },
    source: { countries: 'DB-IP 2026-11', attribution: 'DB-IP', attribution_url: 'https://db-ip.com' },
    version: '0.15.0',
    ...changes,
  };
}

describe('parseOverview', () => {
  it('turns the snake_case answer into the dashboard data and reads null lists as empty', () => {
    const overview = parseOverview(overviewJson());
    expect(overview.resolutionSeconds).toBe(3600);
    expect(overview.series['messages.created']).toEqual([1, 2]);
    expect(overview.totals.refusals).toBe(8);
    expect(overview.previous?.visitors).toBe(3);
    expect(overview.pages).toEqual([]);
    expect(overview.storage).toEqual({ freeBytes: 10, totalBytes: 20, reserveBytes: 5, databaseBytes: 2 });
    expect(overview.security.sessionExpiresAt).toBe(1_700_000_900);
    expect(overview.source.attributionUrl).toBe('https://db-ip.com');
  });

  it('leaves out the earlier span the 12-month range does not keep', () => {
    expect(parseOverview(overviewJson({ range: '12m', previous: undefined })).previous).toBeUndefined();
  });

  it.each([
    ['an unknown range', { range: '1y' }],
    ['series of different lengths', { series: { ...overviewJson().series, visitors: [1] } }],
    ['a missing metric', { totals: { ...METRICS, refusals: undefined } }],
    ['a negative count', { stored: -1 }],
    ['a fractional count in a series', { series: { ...overviewJson().series, refusals: [1, 0.5] } }],
  ])('refuses %s', (_, changes) => {
    expect(() => parseOverview(overviewJson(changes))).toThrow(ResponseError);
  });
});

describe('parseLive', () => {
  const live = {
    online: 2,
    people: [0, 1],
    robots: [3, 0],
    session_expires_at: 1_700_000_900,
    usage_supported: true,
    usage: [{ at: 1, processor: -1, load: 0.25, memory_used: 5, memory_total: 0, process_bytes: 7 }],
    refused: [{ client: '2001:db8::/64', country: '', reasons: null, count: 3, first: 1, last: 2 }],
    refused_version: 4,
  };

  it('keeps unknown processor figures negative and reads null reasons as none', () => {
    const parsed = parseLive(live);
    expect(parsed.usage[0]).toEqual({
      at: 1,
      processor: -1,
      load: 0.25,
      memoryUsed: 5,
      memoryTotal: 0,
      processBytes: 7,
    });
    expect(parsed.refused?.[0]?.reasons).toEqual({});
    expect(parsed.refusedVersion).toBe(4);
    expect(parseLive({ ...live, refused: undefined }).refused).toBeUndefined();
    expect(parsed.sessionExpiresAt).toBe(1_700_000_900);
  });

  it('refuses an answer without usage_supported', () => {
    expect(() => parseLive({ ...live, usage_supported: 'yes' })).toThrow(ResponseError);
  });
});
