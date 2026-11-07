const hour = 60 * 60 * 1000;
export const renovateInterval = 6 * hour;
const windowDays = new Set([1, 3, 5]);
const windowStartMinutes = 13 * 60 + 47;

export interface LastRuns {
  readonly renovate: number | null;
  readonly dependencyWindow: number | null;
}

export interface DueWorkflows {
  readonly renovate: boolean;
  readonly dependencyWindow: boolean;
}

function windowStart(now: number): number | null {
  const today = new Date(now);
  if (!windowDays.has(today.getUTCDay())) {
    return null;
  }
  const start = Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate()) + windowStartMinutes * 60_000;
  return now >= start ? start : null;
}

// GitHub drops many scheduled runs, so the watch is the one scheduler: it starts each periodic workflow whose turn has passed.
export function dueWorkflows(now: number, last: LastRuns): DueWorkflows {
  const window = windowStart(now);
  return {
    renovate: last.renovate === null || now - last.renovate >= renovateInterval,
    dependencyWindow: window !== null && (last.dependencyWindow === null || last.dependencyWindow < window),
  };
}
