import type { Live, Overview, Range, TimelineMetric } from '../api/types.ts';
import { SECONDS_PER_DAY, SECONDS_PER_HOUR } from '../charts/geometry/time.ts';
import type { Timeline } from '../charts/geometry/time.ts';

const SPANS: Readonly<Record<Range, readonly [resolution: number, periods: number]>> = {
  '24h': [SECONDS_PER_HOUR, 24],
  '7d': [SECONDS_PER_HOUR, 7 * 24],
  '30d': [SECONDS_PER_DAY, 30],
  '90d': [SECONDS_PER_DAY, 90],
  '12m': [SECONDS_PER_DAY, 365],
};

function metrics<T>(value: () => T): Record<TimelineMetric, T> {
  return {
    'views.people': value(),
    'views.robots': value(),
    'views.unconfirmed': value(),
    visitors: value(),
    'messages.created': value(),
    'messages.opened': value(),
    'messages.ended': value(),
    'messages.expired': value(),
    refusals: value(),
  };
}

export function emptyOverview(range: Range, now: number): Overview {
  const [resolutionSeconds, periods] = SPANS[range];
  const until = Math.floor(now / resolutionSeconds + 1) * resolutionSeconds;
  return {
    range,
    from: until - periods * resolutionSeconds,
    resolutionSeconds,
    series: metrics(() => Array.from({ length: periods }, () => 0)),
    totals: metrics(() => 0),
    previous: undefined,
    countries: [],
    pages: [],
    robots: [],
    stored: 0,
    storage: { freeBytes: 0, totalBytes: 0, reserveBytes: 0, databaseBytes: 0 },
    security: { passkeys: 0, lastSignIn: 0, sessionExpiresAt: 0 },
    source: { countries: '', attribution: '', attributionUrl: '' },
    version: '',
  };
}

export const EMPTY_LIVE: Live = {
  online: 0,
  people: Array.from({ length: 30 }, () => 0),
  robots: Array.from({ length: 30 }, () => 0),
  sessionExpiresAt: 0,
  usageSupported: true,
  usage: [],
  refused: [],
};

export function timelineOf(overview: Overview): Timeline {
  return {
    from: overview.from,
    resolutionSeconds: overview.resolutionSeconds,
    count: overview.series['views.people'].length,
  };
}

export function trend(current: number, previous: number | undefined): number | undefined {
  if (previous === undefined || previous === 0) {
    return undefined;
  }
  return (current - previous) / previous;
}
