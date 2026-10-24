import { ID_LEN, deriveLink, encodedLength, parseLink, requestKey, utf8, wipeRequestKey, type Link, type LinkKeys, type RequestKey } from '@nylzo/core';

import { ApiError, type Api } from './api.ts';

export const MAX_REASON_BYTES = 1000;
const MAX_REPORT_BODY_BYTES = 4096;
const DECODE_PASSES = 3;
const LINK_ORIGIN = /^([Hh][Tt][Tt][Pp][Ss]?:\/\/[^/?#]+)/;
const PERCENT = 0x25;
const LINK_FRAGMENT = /#[^\t\n\f\r ]*/g;
const TOKEN_RUN = new RegExp(`[A-Za-z0-9_-]{${encodedLength(ID_LEN)},}`, 'g');
const REDACTED = '[redacted]';

export type ReportProblem = 'link_invalid' | 'link_foreign' | 'reason_too_long';

export type ReportOutcome =
  | { readonly outcome: 'accepted' }
  | { readonly outcome: 'invalid'; readonly problems: readonly ReportProblem[] }
  | { readonly outcome: 'retry_later'; readonly retryAfter?: number | undefined }
  | { readonly outcome: 'failed' }
  | { readonly outcome: 'ignored' };

function hexValue(byte: number | undefined): number {
  if (byte === undefined) {
    return -1;
  }
  const digit = String.fromCharCode(byte);
  return /^[0-9A-Fa-f]$/.test(digit) ? parseInt(digit, 16) : -1;
}

function percentDecodePass(bytes: Uint8Array): Uint8Array {
  const out: number[] = [];
  for (let index = 0; index < bytes.length; index++) {
    const high = hexValue(bytes[index + 1]);
    const low = hexValue(bytes[index + 2]);
    if (bytes[index] === PERCENT && high >= 0 && low >= 0) {
      out.push(high * 16 + low);
      index += 2;
    } else {
      out.push(bytes[index] ?? 0);
    }
  }
  return Uint8Array.from(out);
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
  return new TextDecoder().decode(bytes).replaceAll('\uFFFD', '');
}

export function redactReason(reason: string): string {
  return percentDecode(reason)
    .replace(/\p{Cf}/gu, '')
    .replace(/\p{Cc}/gu, ' ')
    .replace(LINK_FRAGMENT, REDACTED)
    .replace(TOKEN_RUN, REDACTED);
}

export function reasonTooLong(reason: string): boolean {
  const body = JSON.stringify({ id: 'A'.repeat(encodedLength(ID_LEN)), reason });
  return utf8(reason).length > MAX_REASON_BYTES || utf8(body).length > MAX_REPORT_BODY_BYTES;
}

export async function sendReport(api: Api, id: Uint8Array, readKey: RequestKey, reason: string): Promise<ReportOutcome> {
  const redacted = redactReason(reason);
  if (reasonTooLong(redacted)) {
    return { outcome: 'invalid', problems: ['reason_too_long'] };
  }
  try {
    await api.report(id, readKey, redacted);
    return { outcome: 'accepted' };
  } catch (error) {
    if (error instanceof ApiError && (error.kind === 'rate_limited' || error.kind === 'unavailable')) {
      return { outcome: 'retry_later', retryAfter: error.retryAfter };
    }
    return { outcome: 'failed' };
  }
}

export async function reportLink(api: Api, origin: string, pasted: string, reason: string): Promise<ReportOutcome> {
  const raw = pasted.trim();
  const problems: ReportProblem[] = [];
  let link: Link | undefined;
  try {
    link = parseLink(raw);
  } catch {
    problems.push('link_invalid');
  }
  if (link && LINK_ORIGIN.exec(raw)?.[1]?.toLowerCase() !== origin.toLowerCase()) {
    problems.push('link_foreign');
  }
  if (reasonTooLong(redactReason(reason))) {
    problems.push('reason_too_long');
  }
  if (!link || problems.length > 0) {
    link?.s.fill(0);
    return { outcome: 'invalid', problems };
  }
  const readKey = await readKeyOf(link).catch(() => undefined);
  try {
    return readKey ? await sendReport(api, link.id, readKey, reason) : { outcome: 'failed' };
  } finally {
    wipeRequestKey(readKey);
  }
}

async function readKeyOf(link: Link): Promise<RequestKey> {
  let keys: LinkKeys | undefined;
  try {
    keys = await deriveLink(link.id, link.s);
    return await requestKey(keys.r);
  } finally {
    link.s.fill(0);
    keys?.saltPw.fill(0);
    keys?.r.fill(0);
  }
}
