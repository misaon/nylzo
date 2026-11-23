import { describe, expect, it } from 'vitest';

import { mediaTypeOf, readBody } from './object-body.ts';

describe('response bodies', () => {
  it('read a response without a body as no bytes', async () => {
    expect(await readBody(new Response(null, { status: 204 }), 16)).toEqual(new Uint8Array(0));
  });

  it.each([
    ['application/json', 'application/json'],
    ['Application/JSON; charset=utf-8', 'application/json'],
    [' application/octet-stream ;x=1;y=2', 'application/octet-stream'],
    ['', ''],
  ])('take the media type of %j as %j', (contentType, mediaType) => {
    expect(mediaTypeOf(new Response(null, { headers: { 'Content-Type': contentType } }))).toBe(mediaType);
  });

  it('take no media type from a response without Content-Type', () => {
    const response = new Response(null);
    response.headers.delete('Content-Type');
    expect(mediaTypeOf(response)).toBe('');
  });
});
