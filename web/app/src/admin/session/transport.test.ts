import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError, ResponseError } from '../../shared/api/api-errors.ts';
import { UnreachableError } from './problems.ts';
import { createTransport } from './transport.ts';
import type { Fetch } from './transport.ts';

interface Exchange {
  readonly path: string;
  readonly init: RequestInit;
}

function jsonResponse(body: BodyInit, contentType = 'application/json; charset=utf-8', status = 200): Response {
  return new Response(body, { status, headers: { 'Content-Type': contentType } });
}

function pathOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') {
    return input;
  }
  return input instanceof URL ? input.href : input.url;
}

function server(answer: () => Response) {
  const exchanges: Exchange[] = [];
  const fetchResource: Fetch = (input, init = {}) => {
    exchanges.push({ path: pathOf(input), init });
    return Promise.resolve(answer());
  };
  return { exchanges, transport: createTransport(fetchResource) };
}

function headersOf(exchange: Exchange | undefined): Headers {
  return new Headers(exchange?.init.headers);
}

describe('createTransport', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('sends a read without a body or the cross-site marker and keeps cookies, cache and redirects out', async () => {
    const { exchanges, transport } = server(() => jsonResponse('{"enrolled":true}'));

    await expect(transport.send('GET', '/api/v1/admin', { headers: { Authorization: 'Nylzo x' } })).resolves.toEqual({
      enrolled: true,
    });

    const [exchange] = exchanges;
    expect(exchange?.path).toBe('/api/v1/admin');
    expect(exchange?.init).toMatchObject({
      method: 'GET',
      credentials: 'omit',
      cache: 'no-store',
      redirect: 'error',
      referrerPolicy: 'no-referrer',
    });
    expect(exchange?.init.body).toBeUndefined();
    expect(exchange?.init.signal).toBeInstanceOf(AbortSignal);
    expect(headersOf(exchange).get('Authorization')).toBe('Nylzo x');
    expect(headersOf(exchange).has('X-Requested-With')).toBe(false);
    expect(headersOf(exchange).has('Content-Type')).toBe(false);
  });

  it('marks every change as a script request and sends its body as JSON', async () => {
    const { exchanges, transport } = server(() => jsonResponse('{}'));

    await transport.send('POST', '/api/v1/admin/session/options', { body: { key: 'abc' } });
    await transport.send('DELETE', '/api/v1/admin/session');

    const [post, removal] = exchanges;
    expect(headersOf(post).get('X-Requested-With')).toBe('XMLHttpRequest');
    expect(headersOf(post).get('Content-Type')).toBe('application/json');
    expect(post?.init.body).toBe('{"key":"abc"}');
    expect(headersOf(removal).get('X-Requested-With')).toBe('XMLHttpRequest');
    expect(headersOf(removal).has('Content-Type')).toBe(false);
    expect(removal?.init.body).toBeUndefined();
  });

  it('uses the global fetch when none is given', async () => {
    const fetchResource = vi.fn<Fetch>(() => Promise.resolve(jsonResponse('{"enrolled":false}')));
    vi.stubGlobal('fetch', fetchResource);

    await expect(createTransport().send('GET', '/api/v1/admin')).resolves.toEqual({ enrolled: false });
    expect(fetchResource).toHaveBeenCalledOnce();
  });

  it('reports a server it cannot reach as unreachable', async () => {
    const failure = new TypeError('network down');
    const transport = createTransport(() => Promise.reject(failure));

    const sent = transport.send('GET', '/api/v1/admin');
    await expect(sent).rejects.toBeInstanceOf(UnreachableError);
    await expect(sent).rejects.toHaveProperty('cause', failure);
  });

  it('turns a refusal into an API error with its status', async () => {
    const { transport } = server(() => jsonResponse('{"error":"rate_limited"}', 'application/json', 429));

    const sent = transport.send('POST', '/api/v1/admin/session', { body: {} });
    await expect(sent).rejects.toBeInstanceOf(ApiError);
    await expect(sent).rejects.toMatchObject({ status: 429, kind: 'rate_limited' });
  });

  it('refuses an answer that is not JSON and does not read its body', async () => {
    const body = new ReadableStream<Uint8Array>();
    const cancel = vi.spyOn(body, 'cancel');
    const { transport } = server(() => jsonResponse(body, 'text/html'));

    await expect(transport.send('GET', '/api/v1/admin')).rejects.toBeInstanceOf(ResponseError);
    expect(cancel).toHaveBeenCalledOnce();
  });

  it('refuses an answer without a media type', async () => {
    const { transport } = server(() => new Response(null, { status: 204 }));

    await expect(transport.send('GET', '/api/v1/admin')).rejects.toBeInstanceOf(ResponseError);
  });

  it.each([
    ['text that is not UTF-8', new Uint8Array([0x7b, 0xff, 0x7d])],
    ['malformed JSON', '{"enrolled":'],
    ['a JSON list', '[true]'],
    ['JSON null', 'null'],
    ['an answer over half a mebibyte', `{"padding":"${'x'.repeat(512 * 1024)}"}`],
  ])('refuses %s', async (_, body) => {
    const { transport } = server(() => jsonResponse(body));

    await expect(transport.send('GET', '/api/v1/admin')).rejects.toBeInstanceOf(ResponseError);
  });
});
