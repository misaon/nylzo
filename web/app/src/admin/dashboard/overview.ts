import { byTimelineMetric } from '../api/types.ts';
import type { Live, Overview, Range } from '../api/types.ts';
import { SECONDS_PER_DAY, SECONDS_PER_HOUR } from '../charts/geometry/time.ts';
import type { Timeline } from '../charts/geometry/time.ts';

interface Span extends Pick<Overview, 'resolutionSeconds'> {
  readonly periods: number;
}

const SPANS: Readonly<Record<Range, Span>> = {
  '24h': { resolutionSeconds: SECONDS_PER_HOUR, periods: 24 },
  '7d': { resolutionSeconds: SECONDS_PER_HOUR, periods: 7 * 24 },
  '30d': { resolutionSeconds: SECONDS_PER_DAY, periods: 30 },
  '90d': { resolutionSeconds: SECONDS_PER_DAY, periods: 90 },
  '12m': { resolutionSeconds: SECONDS_PER_DAY, periods: 365 },
};

export function emptyOverview(range: Range, now: number): Overview {
  const { resolutionSeconds, periods } = SPANS[range];
  const until = Math.floor(now / resolutionSeconds + 1) * resolutionSeconds;
  return {
    range,
    from: until - periods * resolutionSeconds,
    resolutionSeconds,
    series: byTimelineMetric(() => Array.from({ length: periods }, () => 0)),
    totals: byTimelineMetric(() => 0),
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

interface Compared {
  readonly current: number;
  readonly previous: number | undefined;
}

export function trend({ current, previous }: Compared): number | undefined {
  if (previous === undefined || previous === 0) {
    return undefined;
  }
  return (current - previous) / previous;
}
