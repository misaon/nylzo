import {
  deriveLink,
  encodedLength,
  ID_LEN,
  type Link,
  type LinkKeys,
  LinkRetiredKdfError,
  LinkVersionError,
  parseLink,
  requestKey,
  type RequestKey,
  utf8,
  wipeRequestKey,
} from '@nylzo/core';

import type { Api } from '../api/api.ts';

import { shouldRetryLater } from '../api/api-errors.ts';

export const MAX_REASON_BYTES = 1000;
const MAX_REPORT_BODY_BYTES = 4096;
const DECODE_PASSES = 3;
const PERCENT = 0x25;
const LINK_ORIGIN = /^(https?:\/\/[^/?#]+)/i;
const LINK_FRAGMENT = /#[^\t\n\f\r ]*/g;
const TOKEN_RUN = new RegExp(String.raw`[\w-]{${encodedLength(ID_LEN)},}`, 'g');
const HEX_DIGIT = /^[\da-f]$/i;

type ReportProblem = 'link_foreign' | 'link_invalid' | 'link_newer' | 'link_retired' | 'reason_too_long';

export interface ReportSubmission {
  readonly link: string;
  readonly reason: string;
}

export type ReportOutcome =
  | { readonly outcome: 'accepted' }
  | { readonly outcome: 'failed' }
  | { readonly outcome: 'ignored' }
  | { readonly outcome: 'invalid'; readonly problems: readonly ReportProblem[] }
  | { readonly outcome: 'retry_later'; readonly retryAfter?: number | undefined };

function hexValue(byte: number | undefined): number {
  if (byte === undefined) {
    return -1;
  }
  const digit = String.fromCodePoint(byte);
  return HEX_DIGIT.test(digit) ? Number.parseInt(digit, 16) : -1;
}

function percentDecodePass(bytes: Uint8Array): Uint8Array {
  const decoded: number[] = [];
  for (let index = 0; index < bytes.length; index++) {
    const high = hexValue(bytes[index + 1]);
    const low = hexValue(bytes[index + 2]);
    if (bytes[index] === PERCENT && high >= 0 && low >= 0) {
      decoded.push(high * 16 + low);
      index += 2;
    } else {
      decoded.push(bytes[index] ?? 0);
    }
  }
  return Uint8Array.from(decoded);
}

function percentDecode(text: string): string {
  let bytes: Uint8Array = utf8(text);
  for (let pass = 0; pass < DECODE_PASSES; pass++) {
    const decoded = percentDecodePass(bytes);
    if (decoded.length === bytes.length) {
      break;
    }
    bytes = decoded;
  }
  return new TextDecoder().decode(bytes).replaceAll('\u{FFFD}', '');
}

export function redactReason(reason: string): string {
  return percentDecode(reason)
    .replaceAll(/\p{Cf}/gu, '')
    .replaceAll(/\p{Cc}/gu, ' ')
    .replaceAll(LINK_FRAGMENT, '[redacted]')
    .replaceAll(TOKEN_RUN, '[redacted]');
}

export function reasonTooLong(reason: string): boolean {
  const body = JSON.stringify({ id: 'A'.repeat(encodedLength(ID_LEN)), reason });
  return utf8(reason).length > MAX_REASON_BYTES || utf8(body).length > MAX_REPORT_BODY_BYTES;
}

export async function sendReport(
  api: Api,
  id: Uint8Array,
  readKey: RequestKey,
  reason: string,
): Promise<ReportOutcome> {
  const redacted = redactReason(reason);
  if (reasonTooLong(redacted)) {
    return { outcome: 'invalid', problems: ['reason_too_long'] };
  }
  try {
    await api.report(id, readKey, redacted);
    return { outcome: 'accepted' };
  } catch (error) {
    return shouldRetryLater(error) ? { outcome: 'retry_later', retryAfter: error.retryAfter } : { outcome: 'failed' };
  }
}

function linkProblem(error: unknown): ReportProblem {
  if (error instanceof LinkVersionError) {
    return 'link_newer';
  }
  return error instanceof LinkRetiredKdfError ? 'link_retired' : 'link_invalid';
}

async function readKeyFrom(link: Link): Promise<RequestKey | undefined> {
  let keys: LinkKeys | undefined;
  try {
    keys = await deriveLink(link.id, link.s);
    return await requestKey(keys.r);
  } catch {
    return undefined;
  } finally {
    link.s.fill(0);
    keys?.saltPw.fill(0);
    keys?.r.fill(0);
  }
}

export async function reportLink(api: Api, origin: string, pasted: string, reason: string): Promise<ReportOutcome> {
  const trimmed = pasted.trim();
  const problems: ReportProblem[] = [];
  let link: Link | undefined;
  try {
    link = parseLink(trimmed);
  } catch (error) {
    problems.push(linkProblem(error));
  }
  if (link && LINK_ORIGIN.exec(trimmed)?.[1]?.toLowerCase() !== origin.toLowerCase()) {
    problems.push('link_foreign');
  }
  if (reasonTooLong(redactReason(reason))) {
    problems.push('reason_too_long');
  }
  if (!link || problems.length > 0) {
    link?.s.fill(0);
    return { outcome: 'invalid', problems };
  }
  const readKey = await readKeyFrom(link);
  if (!readKey) {
    return { outcome: 'failed' };
  }
  try {
    return await sendReport(api, link.id, readKey, reason);
  } finally {
    wipeRequestKey(readKey);
  }
}
