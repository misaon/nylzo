import { mkdirSync, rmSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

import type { Page } from '@playwright/test';

import { ADMIN_BASE_URL, serverCommand } from './servers.ts';

export async function addPasskeyAuthenticator(page: Page): Promise<void> {
  const session = await page.context().newCDPSession(page);
  await session.send('WebAuthn.enable');
  await session.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2',
      transport: 'internal',
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      automaticPresenceSimulation: true,
    },
  });
}

const ENROLLMENT_LOCK = path.join(tmpdir(), `nylzo-e2e-enrollment-${new URL(ADMIN_BASE_URL).port}`);
const LOCK_WAIT_MS = 50;
const STALE_LOCK_MS = 60_000;

function takeEnrollmentLock(): boolean {
  try {
    mkdirSync(ENROLLMENT_LOCK);
    return true;
  } catch {
    const taken = statSync(ENROLLMENT_LOCK, { throwIfNoEntry: false });
    if (taken && Date.now() - taken.mtimeMs > STALE_LOCK_MS) {
      rmSync(ENROLLMENT_LOCK, { recursive: true, force: true });
    }
    return false;
  }
}

// A newer enrollment link replaces the one before it, so the tests of all workers enroll one after another.
export async function withEnrollmentLink<Result>(use: (link: string) => Promise<Result>): Promise<Result> {
  while (!takeEnrollmentLock()) {
    await delay(LOCK_WAIT_MS);
  }
  try {
    return await use(serverCommand(ADMIN_BASE_URL, ['admin-enroll']).trim());
  } finally {
    rmSync(ENROLLMENT_LOCK, { recursive: true, force: true });
  }
}
