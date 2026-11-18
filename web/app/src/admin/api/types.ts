export const RANGES = ['24h', '7d', '30d', '90d', '12m'] as const;

export type Range = (typeof RANGES)[number];

export const TIMELINE_METRICS = [
  'views.people',
  'views.robots',
  'views.unconfirmed',
  'visitors',
  'messages.created',
  'messages.opened',
  'messages.ended',
  'messages.expired',
  'refusals',
] as const;

export type TimelineMetric = (typeof TIMELINE_METRICS)[number];

export interface Share {
  readonly name: string;
  readonly value: number;
}

export interface Overview {
  readonly range: Range;
  /** Unix seconds of the first period. */
  readonly from: number;
  /** Length of a period: 3600 for hours, 86400 for UTC days. */
  readonly resolutionSeconds: number;
  /** One value per period, oldest first; every metric has the same length. */
  readonly series: Readonly<Record<TimelineMetric, readonly number[]>>;
  readonly totals: Readonly<Record<TimelineMetric, number>>;
  /** Totals of the span before this one; absent when the server does not keep it (the 12-month range). */
  readonly previous: Readonly<Record<TimelineMetric, number>> | undefined;
  /** Visitors by ISO 3166-1 alpha-2 code, or "unknown", largest first. */
  readonly countries: readonly Share[];
  /** Views of people by page: home, sent, transparency, terms, agents, other. */
  readonly pages: readonly Share[];
  /** Views of robots by family: search, ai, preview, monitor, audit, script, headless, flood, other. */
  readonly robots: readonly Share[];
  /** Messages the server holds right now. */
  readonly stored: number;
  readonly storage: {
    readonly freeBytes: number;
    readonly totalBytes: number;
    /** Free space below which the server refuses new messages. */
    readonly reserveBytes: number;
    readonly databaseBytes: number;
  };
  readonly security: {
    readonly passkeys: number;
    /** Unix seconds of the last sign-in, 0 when none. */
    readonly lastSignIn: number;
    readonly sessionExpiresAt: number;
  };
  readonly source: {
    readonly countries: string;
    readonly attribution: string;
    readonly attributionUrl: string;
  };
  readonly version: string;
}

export interface UsagePoint {
  /** Unix seconds. */
  readonly at: number;
  /** Share of all cores that was busy, 0 to 1; negative when unknown. */
  readonly processor: number;
  /** One-minute load average per core; negative when unknown. */
  readonly load: number;
  readonly memoryUsed: number;
  /** 0 when unknown. */
  readonly memoryTotal: number;
  /** Memory the server process holds. */
  readonly processBytes: number;
}

export interface RefusedClient {
  /** An IPv4 address or an IPv6 /64. */
  readonly client: string;
  /** ISO 3166-1 alpha-2 code, or '' when unknown. */
  readonly country: string;
  /** How often each limit refused the client: requests, create, read, read-bytes, admin. */
  readonly reasons: Readonly<Record<string, number>>;
  readonly count: number;
  readonly first: number;
  readonly last: number;
}

export interface Live {
  /** People with a sign of life in the last five minutes. */
  readonly online: number;
  /** Views per minute over the last 30 minutes, oldest first. */
  readonly people: readonly number[];
  readonly robots: readonly number[];
  readonly sessionExpiresAt: number;
  readonly usageSupported: boolean;
  /** One sample every five seconds, oldest first, at most five minutes. */
  readonly usage: readonly UsagePoint[];
  /** Clients a limit refused within the last hour, latest first. */
  readonly refused: readonly RefusedClient[];
}

/** One answer of the live route: the usage samples after the newest the page holds, the refused clients only when they changed. */
export interface LiveUpdate extends Omit<Live, 'refused'> {
  readonly refused: readonly RefusedClient[] | undefined;
  readonly refusedVersion: number | undefined;
}
