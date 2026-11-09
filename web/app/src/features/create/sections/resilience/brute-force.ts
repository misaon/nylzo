import type { Translator } from '../../../../shared/language/translator.svelte.ts';
import { TTL_CHOICES } from '../../../../shared/ttl/ttl-choices.ts';

export type Attacker = 'laptop' | 'gpu' | 'farm' | 'datacenter' | 'world';

export const ATTACKERS: readonly { readonly id: Attacker; readonly rate: number; readonly hypothetical?: boolean }[] = [
  { id: 'laptop', rate: 8 },
  { id: 'gpu', rate: 10 },
  { id: 'farm', rate: 13 },
  { id: 'datacenter', rate: 16 },
  { id: 'world', rate: 21, hypothetical: true },
];

type Verdict = 'week' | 'ttl' | 'life' | 'dinosaurs' | 'earth' | 'universe' | 'beyond';

const SECONDS_PER_YEAR = 31_557_600;
const KEY_BITS = 256n;
const QUANTUM_BITS = 128n;
const QUANTUM_NUMERATOR = 785_398_163n;
const QUANTUM_DENOMINATOR = 1_000_000_000n;
const UNIVERSE_YEARS = 13.8e9;
const FAR_BEYOND = 3;
const MILESTONES: readonly { readonly years: number; readonly verdict: Verdict }[] = [
  { years: Math.max(...TTL_CHOICES) / SECONDS_PER_YEAR, verdict: 'ttl' },
  { years: 80, verdict: 'life' },
  { years: 66e6, verdict: 'dinosaurs' },
  { years: 4.5e9, verdict: 'earth' },
  { years: UNIVERSE_YEARS, verdict: 'universe' },
];

function rateOf(attacker: Attacker): number {
  return ATTACKERS.find((item) => item.id === attacker)?.rate ?? 0;
}

function averageYearsLog10(attacker: Attacker, quantum: boolean): number {
  const log10OfTwo = Math.log10(2);
  const steps = quantum
    ? Number(QUANTUM_BITS) * log10OfTwo + Math.log10(Math.PI / 4)
    : (Number(KEY_BITS) - 1) * log10OfTwo;
  return steps - rateOf(attacker) - Math.log10(SECONDS_PER_YEAR);
}

export interface Judgement {
  readonly verdict: Verdict;
  readonly bigBang: boolean;
  readonly yearsLog10: number;
}

export function judge(attacker: Attacker, quantum: boolean): Judgement {
  const yearsLog10 = averageYearsLog10(attacker, quantum);
  const passed = MILESTONES.filter((milestone) => yearsLog10 > Math.log10(milestone.years));
  const universe = Math.log10(UNIVERSE_YEARS);
  const last = passed.at(-1)?.verdict ?? 'week';
  if (yearsLog10 > universe) {
    return { verdict: yearsLog10 - universe > FAR_BEYOND ? 'beyond' : last, bigBang: true, yearsLog10 };
  }
  return { verdict: last, bigBang: false, yearsLog10 };
}

const LARGE_YEARS = 1e6;

export function yearsText(yearsLog10: number, translator: Translator): string {
  const { language, translate } = translator;
  const years = 10 ** yearsLog10;
  if (years < LARGE_YEARS) {
    return new Intl.NumberFormat(language, { style: 'unit', unit: 'year', unitDisplay: 'long' }).format(
      Math.round(years),
    );
  }
  const amount = new Intl.NumberFormat(language, {
    notation: 'compact',
    compactDisplay: 'long',
    maximumSignificantDigits: 2,
  }).format(years);
  return translate('brute.years.large', { amount });
}

export interface Countdown {
  readonly head: string;
  readonly tail: string;
  readonly done: string;
}

function changedDigits(after: string, before: string): number {
  let same = 0;
  while (same < after.length && after[same] === before[same]) {
    same++;
  }
  return after.length - same;
}

function grouped(value: bigint): string {
  return value.toString().replaceAll(/\B(?=(?:\d{3})+(?!\d))/gu, ' ');
}

export function countdown(attacker: Attacker, quantum: boolean, elapsedMs: number): Countdown {
  const total = quantum ? (2n ** QUANTUM_BITS * QUANTUM_NUMERATOR) / QUANTUM_DENOMINATOR : 2n ** KEY_BITS;
  const done = (10n ** BigInt(rateOf(attacker)) * BigInt(Math.max(0, Math.floor(elapsedMs)))) / 1000n;
  const left = total - done;
  const text = grouped(left);
  let cut = text.length;
  for (let digits = changedDigits(left.toString(), total.toString()); digits > 0; cut--) {
    if (text[cut - 1] !== ' ') {
      digits--;
    }
  }
  return { head: text.slice(0, cut), tail: text.slice(cut), done: grouped(done) };
}
