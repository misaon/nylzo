import { createHash } from 'node:crypto';

import { BEACON_INTEGRITY, BEACON_URL, BEACON_VERSION } from '../src/shared/web-analytics/beacon.ts';

const MOST_BYTES = 1_000_000;

const response = await fetch(BEACON_URL, { redirect: 'error', signal: AbortSignal.timeout(30_000) });
const body = new Uint8Array(await response.arrayBuffer());
if (!response.ok || body.byteLength > MOST_BYTES) {
  throw new Error(`${BEACON_URL} answered ${response.status} with ${body.byteLength} bytes`);
}
const integrity = `sha384-${createHash('sha384').update(body).digest('base64')}`;
const version = /versions:\{js:"([^"]{1,40})"\}/u.exec(new TextDecoder().decode(body))?.[1] ?? 'an unknown version';
if (integrity === BEACON_INTEGRITY) {
  console.log(`Cloudflare still serves the pinned beacon ${BEACON_VERSION}.`);
} else {
  console.error(
    `::error::Cloudflare replaced beacon ${BEACON_VERSION} with ${version} (${integrity}). Browsers refuse it, so nylzo.com counts no visits until a release pins the reviewed file in web/app/src/shared/web-analytics/beacon.ts.`,
  );
  process.exitCode = 1;
}
