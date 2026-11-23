import type { Live, LiveUpdate, UsagePoint } from './types.ts';

// The server keeps five minutes of samples, one every five seconds.
const USAGE_SHOWN = 60;
const USAGE_WINDOW_SECONDS = 300;

export interface HeldLive {
  readonly live: Live;
  readonly refusedVersion: number | undefined;
}

export function liveQuery(held: HeldLive | undefined): string {
  if (held === undefined) {
    return '';
  }
  const query = new URLSearchParams();
  const newest = held.live.usage.at(-1);
  if (newest !== undefined) {
    query.set('since', String(newest.at));
  }
  if (held.refusedVersion !== undefined) {
    query.set('refused', String(held.refusedVersion));
  }
  const text = query.toString();
  return text === '' ? '' : `?${text}`;
}

// The server sends every sample again after a gap or a clock step, so the held samples from its first one on go, and
// the chart keeps only the five minutes before the newest sample.
function mergedUsage({ held, fresh }: Record<'fresh' | 'held', readonly UsagePoint[]>): UsagePoint[] {
  const first = fresh[0]?.at ?? Number.POSITIVE_INFINITY;
  const merged = [...held.filter((sample) => sample.at < first), ...fresh];
  const newest = merged.at(-1)?.at ?? 0;
  return merged
    .filter((sample) => sample.at > newest - USAGE_WINDOW_SECONDS && sample.at <= newest)
    .slice(-USAGE_SHOWN);
}

export function mergeLive(held: HeldLive | undefined, update: LiveUpdate): HeldLive {
  return {
    live: {
      online: update.online,
      people: update.people,
      robots: update.robots,
      sessionExpiresAt: update.sessionExpiresAt,
      usageSupported: update.usageSupported,
      usage: mergedUsage({ held: held?.live.usage ?? [], fresh: update.usage }),
      refused: update.refused ?? held?.live.refused ?? [],
    },
    refusedVersion: update.refusedVersion,
  };
}
