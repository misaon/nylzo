import type { Page, Request } from '@playwright/test';

import { expect } from './fixtures.ts';

export interface RecordedRequest {
  readonly url: string;
  readonly headers: Record<string, string>;
  readonly body: string;
}

export function recordRequests(page: Page): () => Promise<RecordedRequest[]> {
  const headers = new Map<Request, Promise<Record<string, string>>>();
  page.on('request', (request) => {
    headers.set(request, Promise.resolve(request.headers()));
  });
  page.on('response', (response) => {
    headers.set(response.request(), response.request().allHeaders());
  });
  return () =>
    Promise.all(
      [...headers].map(async ([request, sent]) => ({
        url: request.url(),
        headers: await sent,
        body: request.postDataBuffer()?.toString('latin1') ?? '',
      })),
    );
}

export function encodedForms(bytes: Uint8Array): string[] {
  const buffer = Buffer.from(bytes);
  const base64 = buffer.toString('base64');
  return [buffer.toString('hex'), base64, base64.replace(/=+$/, ''), buffer.toString('base64url')];
}

export function textForms(text: string): string[] {
  const utf8 = Buffer.from(text, 'utf8');
  return [text, encodeURIComponent(text), text.replaceAll(' ', '+'), utf8.toString('latin1'), ...encodedForms(utf8)];
}

export function expectNoSecretSent(requests: readonly RecordedRequest[], secrets: readonly string[]): void {
  const forms = secrets.map((secret) => secret.toLowerCase());
  for (const request of requests) {
    const hexBody = Buffer.from(request.body, 'latin1').toString('hex');
    const sent = `${request.url}\n${JSON.stringify(request.headers)}\n${request.body}\n${hexBody}`.toLowerCase();
    for (const form of forms) {
      expect(sent, `${request.url} sends ${form}`).not.toContain(form);
    }
    expect(request.url).not.toContain('#');
    expect(request.headers['referer'] ?? '', request.url).toBe('');
  }
}
