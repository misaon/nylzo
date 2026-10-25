import type { Language } from '../../../shared/language/language.ts';

import { TTL_CHOICES } from '../composer/create-form.ts';

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
  const log2 = Math.log10(2);
  const steps = quantum ? Number(QUANTUM_BITS) * log2 + Math.log10(Math.PI / 4) : (Number(KEY_BITS) - 1) * log2;
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

type YearsKey =
  | 'brute.years.billion.fraction'
  | 'brute.years.billion'
  | 'brute.years.million.fraction'
  | 'brute.years.million'
  | 'brute.years.one'
  | 'brute.years.few'
  | 'brute.years.other';

export interface YearsAmount {
  readonly key: YearsKey;
  readonly amount: string;
}

const PLURAL_KEYS: Partial<Record<Intl.LDMLPluralRule, YearsKey>> = {
  one: 'brute.years.one',
  few: 'brute.years.few',
};

const SCALES: readonly { readonly size: number; readonly fraction: YearsKey; readonly whole: YearsKey }[] = [
  { size: 1e9, fraction: 'brute.years.billion.fraction', whole: 'brute.years.billion' },
  { size: 1e6, fraction: 'brute.years.million.fraction', whole: 'brute.years.million' },
];

export function yearsAmount(yearsLog10: number, language: Language): YearsAmount {
  const years = 10 ** yearsLog10;
  const number = (value: number, digits: number) =>
    new Intl.NumberFormat(language, { maximumFractionDigits: digits, minimumFractionDigits: digits }).format(value);
  for (const { size, fraction, whole } of SCALES) {
    if (years >= size) {
      const value = years / size;
      return value < 10
        ? { key: fraction, amount: number(value, 1) }
        : { key: whole, amount: number(Math.round(value), 0) };
    }
  }
  const rounded = Math.round(years);
  const rule = new Intl.PluralRules(language).select(rounded);
  return { key: PLURAL_KEYS[rule] ?? 'brute.years.other', amount: number(rounded, 0) };
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
  return value.toString().replaceAll(/\B(?=(?:\d{3})+(?!\d))/g, ' ');
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
