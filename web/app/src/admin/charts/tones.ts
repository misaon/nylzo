export type Tone = 'people' | 'visitors' | 'robots' | 'quiet' | 'opened' | 'ended' | 'danger';

export interface ChartSeries {
  readonly label: string;
  readonly values: readonly number[];
  readonly tone: Tone;
}

export interface BarItem {
  readonly key: string;
  readonly label: string;
  readonly value: number;
}

export interface TooltipRow {
  readonly label: string;
  readonly value: string;
  readonly tone: Tone;
}

export const FILLS: Readonly<Record<Tone, string>> = {
  people: 'fill-aegis-400',
  visitors: 'fill-aegis-200',
  robots: 'fill-nebula-400',
  quiet: 'fill-ink-mute/45',
  opened: 'fill-aegis-200',
  ended: 'fill-nebula-300',
  danger: 'fill-rose-400',
};

export const STROKES: Readonly<Record<Tone, string>> = {
  people: 'stroke-aegis-300',
  visitors: 'stroke-aegis-200',
  robots: 'stroke-nebula-400',
  quiet: 'stroke-ink-mute',
  opened: 'stroke-aegis-200',
  ended: 'stroke-nebula-300',
  danger: 'stroke-rose-400',
};

export const STOPS: Readonly<Record<Tone, string>> = {
  people: '[stop-color:var(--color-aegis-400)]',
  visitors: '[stop-color:var(--color-aegis-200)]',
  robots: '[stop-color:var(--color-nebula-400)]',
  quiet: '[stop-color:var(--color-ink-mute)]',
  opened: '[stop-color:var(--color-aegis-200)]',
  ended: '[stop-color:var(--color-nebula-300)]',
  danger: '[stop-color:var(--color-rose-400)]',
};

export const SWATCHES: Readonly<Record<Tone, string>> = {
  people: 'bg-aegis-400 shadow-[0_0_10px_oklch(0.67_0.16_252/0.8)]',
  visitors: 'bg-aegis-200',
  robots: 'bg-nebula-400 shadow-[0_0_10px_oklch(0.72_0.14_305/0.7)]',
  quiet: 'bg-ink-mute/55',
  opened: 'bg-aegis-200',
  ended: 'bg-nebula-300',
  danger: 'bg-rose-400 shadow-[0_0_10px_oklch(0.74_0.14_355/0.7)]',
};
