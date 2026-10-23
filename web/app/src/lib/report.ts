import { ID_LEN, deriveLink, encodedLength, parseLink, requestKey, utf8, type Link, type RequestKey } from '@nylzo/core';

import { ApiError, type Api } from './api.ts';

export const MAX_REASON_BYTES = 1000;
const MAX_REPORT_BODY_BYTES = 4096;
const LINK_ORIGIN = /^([Hh][Tt][Tt][Pp][Ss]?:\/\/[^/?#]+)/;

export type ReportProblem = 'link_invalid' | 'link_foreign' | 'reason_too_long';

export type ReportOutcome =
  | { readonly outcome: 'accepted' }
  | { readonly outcome: 'invalid'; readonly problems: readonly ReportProblem[] }
  | { readonly outcome: 'retry_later'; readonly retryAfter?: number | undefined }
  | { readonly outcome: 'failed' }
  | { readonly outcome: 'ignored' };

export function reasonTooLong(reason: string): boolean {
  const body = JSON.stringify({ id: 'A'.repeat(encodedLength(ID_LEN)), reason });
  return utf8(reason).length > MAX_REASON_BYTES || utf8(body).length > MAX_REPORT_BODY_BYTES;
}

export async function sendReport(api: Api, id: Uint8Array, readKey: RequestKey, reason: string): Promise<ReportOutcome> {
  if (reasonTooLong(reason)) {
    return { outcome: 'invalid', problems: ['reason_too_long'] };
  }
  try {
    await api.report(id, readKey, reason);
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
  if (reasonTooLong(reason)) {
    problems.push('reason_too_long');
  }
  if (!link || problems.length > 0) {
    link?.s.fill(0);
    return { outcome: 'invalid', problems };
  }
  const keys = await deriveLink(link.id, link.s);
  link.s.fill(0);
  keys.saltPw.fill(0);
  const readKey = await requestKey(keys.r);
  keys.r.fill(0);
  return sendReport(api, link.id, readKey, reason);
}
