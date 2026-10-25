import { MIN_TTL_SECONDS } from '@nylzo/core';

import type { InstanceLimits } from '../../../shared/api/responses.ts';
import type { MessageKey } from '../../../shared/language/czech.ts';
import type { CreateFailure, CreateProblem, FileInput } from './create-message.ts';

export const TTL_CHOICES = [3600, 86_400, 259_200, 604_800] as const;

type TtlChoice = (typeof TTL_CHOICES)[number];

function isTtlChoice(seconds: number): seconds is TtlChoice {
  return (TTL_CHOICES as readonly number[]).includes(seconds);
}

export function ttlChoices(limits: InstanceLimits): number[] {
  const choices = new Set<number>(TTL_CHOICES.filter((seconds) => seconds <= limits.maxTtlSeconds));
  if (limits.defaultTtlSeconds >= MIN_TTL_SECONDS && limits.defaultTtlSeconds <= limits.maxTtlSeconds) {
    choices.add(limits.defaultTtlSeconds);
  }
  return [...choices].sort((a, b) => a - b);
}

export function ttlKey(seconds: number): MessageKey | undefined {
  return isTtlChoice(seconds) ? `ttl.${seconds}` : undefined;
}

export function problemKey(problem: CreateProblem): MessageKey {
  switch (problem.field) {
    case 'content': {
      return 'problem.content.missing';
    }
    case 'text': {
      return 'problem.text.too_large';
    }
    case 'file': {
      return 'problem.file.too_large';
    }
    case 'ttl': {
      return 'problem.ttl.out_of_range';
    }
    case 'opens': {
      return 'problem.opens.out_of_range';
    }
    case 'passphrase': {
      return `problem.passphrase.${problem.code}`;
    }
  }
}

export function failureKey(failure: CreateFailure): MessageKey {
  return failure.kind === 'invalid' ? 'failure.failed' : `failure.${failure.kind}`;
}

export type Attachment = { readonly file?: FileInput | undefined } | { readonly problem: CreateProblem };

export async function readAttachment(
  file: (Blob & { readonly name: string }) | undefined,
  limits: InstanceLimits,
): Promise<Attachment> {
  if (!file) {
    return { file: undefined };
  }
  if (file.size > limits.maxFileBytes) {
    return { problem: { field: 'file', code: 'too_large' } };
  }
  return { file: { name: file.name, mime: file.type, bytes: new Uint8Array(await file.arrayBuffer()) } };
}
