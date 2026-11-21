import { ResponseError } from '../../shared/api/api-errors.ts';
import { integer, isObject, text } from '../../shared/api/responses.ts';
import type { Json } from '../../shared/api/responses.ts';
import { byTimelineMetric, RANGES, TIMELINE_METRICS } from '../api/types.ts';
import type { LiveUpdate, Overview, Range, RefusedClient, Share, TimelineMetric, UsagePoint } from '../api/types.ts';

function objectAt(json: Json, key: string): Json {
  const value = json[key];
  if (!isObject(value)) {
    throw new ResponseError(`field ${key} is not an object`);
  }
  return value;
}

// Go writes an empty slice or map it never filled as null.
function listAt(json: Json, key: string): readonly unknown[] {
  const value = json[key];
  if (value === null) {
    return [];
  }
  if (!Array.isArray(value)) {
    throw new ResponseError(`field ${key} is not a list`);
  }
  return value;
}

function counts(json: Json, key: string): number[] {
  return listAt(json, key).map((value) => {
    if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) {
      throw new ResponseError(`field ${key} holds a value that is not a count`);
    }
    return value;
  });
}

function finite(json: Json, key: string): number {
  const value = json[key];
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new ResponseError(`field ${key} is not a number`);
  }
  return value;
}

function rangeOf(json: Json): Range {
  const value = json['range'];
  const found = RANGES.find((range) => range === value);
  if (!found) {
    throw new ResponseError('unknown range');
  }
  return found;
}

function metrics(json: Json): Record<TimelineMetric, number> {
  return byTimelineMetric((metric) => integer(json, metric));
}

function shares(json: Json, key: string): Share[] {
  return listAt(json, key).map((entry) => {
    if (!isObject(entry)) {
      throw new ResponseError(`an entry of ${key} is not an object`);
    }
    return { name: text(entry, 'name'), value: integer(entry, 'value') };
  });
}

export function parseOverview(json: Json): Overview {
  const seriesJson = objectAt(json, 'series');
  const series = byTimelineMetric((metric) => counts(seriesJson, metric));
  const periods = series['views.people'].length;
  if (TIMELINE_METRICS.some((metric) => series[metric].length !== periods)) {
    throw new ResponseError('the series differ in length');
  }
  const storage = objectAt(json, 'storage');
  const security = objectAt(json, 'security');
  const source = objectAt(json, 'source');
  const previous = json['previous'];
  return {
    range: rangeOf(json),
    from: integer(json, 'from'),
    resolutionSeconds: integer(json, 'resolution_s'),
    series,
    totals: metrics(objectAt(json, 'totals')),
    previous: previous === undefined || previous === null ? undefined : metrics(objectAt(json, 'previous')),
    countries: shares(json, 'countries'),
    pages: shares(json, 'pages'),
    robots: shares(json, 'robots'),
    stored: integer(json, 'stored'),
    storage: {
      freeBytes: integer(storage, 'free_bytes'),
      totalBytes: integer(storage, 'total_bytes'),
      reserveBytes: integer(storage, 'reserve_bytes'),
      databaseBytes: integer(storage, 'database_bytes'),
    },
    security: {
      passkeys: integer(security, 'passkeys'),
      lastSignIn: integer(security, 'last_sign_in'),
      sessionExpiresAt: integer(security, 'session_expires_at'),
    },
    source: {
      countries: text(source, 'countries'),
      attribution: text(source, 'attribution'),
      attributionUrl: text(source, 'attribution_url'),
    },
    version: text(json, 'version'),
  };
}

function usagePoint(entry: unknown): UsagePoint {
  if (!isObject(entry)) {
    throw new ResponseError('a usage sample is not an object');
  }
  return {
    at: integer(entry, 'at'),
    processor: finite(entry, 'processor'),
    load: finite(entry, 'load'),
    memoryUsed: integer(entry, 'memory_used'),
    memoryTotal: integer(entry, 'memory_total'),
    processBytes: integer(entry, 'process_bytes'),
  };
}

function reasons(entry: Json): Record<string, number> {
  const value = entry['reasons'];
  if (value === null || value === undefined) {
    return {};
  }
  if (!isObject(value)) {
    throw new ResponseError('field reasons is not an object');
  }
  return Object.fromEntries(Object.keys(value).map((reason) => [reason, integer(value, reason)]));
}

function refusedClient(entry: unknown): RefusedClient {
  if (!isObject(entry)) {
    throw new ResponseError('a refused client is not an object');
  }
  return {
    client: text(entry, 'client'),
    country: text(entry, 'country'),
    reasons: reasons(entry),
    count: integer(entry, 'count'),
    first: integer(entry, 'first'),
    last: integer(entry, 'last'),
  };
}

export function parseLive(json: Json): LiveUpdate {
  const usageSupported = json['usage_supported'];
  if (typeof usageSupported !== 'boolean') {
    throw new ResponseError('field usage_supported is not a boolean');
  }
  return {
    online: integer(json, 'online'),
    people: counts(json, 'people'),
    robots: counts(json, 'robots'),
    sessionExpiresAt: integer(json, 'session_expires_at'),
    usageSupported,
    usage: listAt(json, 'usage').map((entry) => usagePoint(entry)),
    refused: json['refused'] === undefined ? undefined : listAt(json, 'refused').map((entry) => refusedClient(entry)),
    refusedVersion: json['refused_version'] === undefined ? undefined : integer(json, 'refused_version'),
  };
}
