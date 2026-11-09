import { MIN_TTL_SECONDS } from '@nylzo/core';

import type { InstanceLimits } from '../api/responses.ts';
import type { MessageKey } from '../language/catalog.ts';

export const SECONDS_PER_DAY = 86_400;

export const TTL_CHOICES = [3600, 86_400, 259_200, 604_800] as const;

export const LONGEST_TTL_DAYS = Math.max(...TTL_CHOICES) / SECONDS_PER_DAY;

type TtlChoice = (typeof TTL_CHOICES)[number];

function isTtlChoice(seconds: number): seconds is TtlChoice {
  return (TTL_CHOICES as readonly number[]).includes(seconds);
}

export function ttlChoices(limits: InstanceLimits): number[] {
  const choices = new Set<number>(TTL_CHOICES.filter((seconds) => seconds <= limits.maxTtlSeconds));
  if (limits.defaultTtlSeconds >= MIN_TTL_SECONDS && limits.defaultTtlSeconds <= limits.maxTtlSeconds) {
    choices.add(limits.defaultTtlSeconds);
  }
  return [...choices].toSorted((first, second) => first - second);
}

export function ttlKey(seconds: number): MessageKey | undefined {
  return isTtlChoice(seconds) ? `ttl.${seconds}` : undefined;
}
