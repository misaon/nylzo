import type { InstanceLimits } from '../../../shared/api/responses.ts';
import type { MessageKey } from '../../../shared/language/catalog.ts';
import type { CreateFailure, CreateProblem, FileInput } from '../create-message.ts';

export function problemKey(problem: CreateProblem): MessageKey {
  switch (problem.field) {
    case 'content': {
      return 'problem.content.missing';
    }
    case 'text': {
      return 'problem.text.too_large';
    }
    case 'file': {
      return `problem.file.${problem.code}`;
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

export type ChosenFile = { readonly file?: FileInput | undefined } | { readonly problem: CreateProblem };

export async function readAttachment(
  file: (Blob & { readonly name: string }) | undefined,
  limits: InstanceLimits,
): Promise<ChosenFile> {
  if (!file) {
    return { file: undefined };
  }
  if (file.size > limits.maxFileBytes) {
    return { problem: { field: 'file', code: 'too_large' } };
  }
  try {
    return { file: { name: file.name, mime: file.type, bytes: new Uint8Array(await file.arrayBuffer()) } };
  } catch {
    return { problem: { field: 'file', code: 'unreadable' } };
  }
}
