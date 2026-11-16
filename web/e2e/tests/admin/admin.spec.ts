import type { Page } from '@playwright/test';

import { addPasskeyAuthenticator, withEnrollmentLink } from '../support/admin.ts';
import { expect, test } from '../support/fixtures.ts';
import { openApp } from '../support/pages.ts';
import { POW_BASE_URL } from '../support/servers.ts';
import { watchViolations } from '../support/watchers.ts';

function watchConsoleErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      errors.push(message.text());
    }
  });
  return errors;
}

async function expectSignedIn(page: Page): Promise<void> {
  const overview = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/admin/overview');
  const live = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/admin/live');
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const [overviewAnswer, liveAnswer] = await Promise.all([overview, live]);
  expect(overviewAnswer.status()).toBe(200);
  expect(liveAnswer.status()).toBe(200);
}

test('an operator enrolls a passkey with the link of admin-enroll, signs in again after a reload and signs out', async ({
  newPage,
}) => {
  const page = await newPage();
  const errors = watchConsoleErrors(page);
  const violations = await watchViolations(page);
  await addPasskeyAuthenticator(page);
  const link = await withEnrollmentLink(async (issued) => {
    expect(issued).toMatch(/^http:\/\/localhost:\d+\/admin#enroll=[\w-]{32}$/u);
    await openApp(page, issued);
    expect(new URL(page.url()).hash).toBe('');
    await expect(page.getByRole('heading', { level: 1, name: 'Create a passkey for the admin area' })).toBeVisible();
    const signedIn = expectSignedIn(page);
    await page.getByRole('button', { name: 'Create passkey' }).click();
    await signedIn;
    return issued;
  });
  await expect(page.locator('main')).not.toContainText('Sign in with passkey');

  await page.reload();
  await page.locator('html[data-ready]').waitFor({ state: 'attached' });
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to the admin area' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Sign out' })).toHaveCount(0);
  const signedInAgain = expectSignedIn(page);
  await page.getByRole('button', { name: 'Sign in with passkey' }).click();
  await signedInAgain;

  const signedOut = page.waitForResponse(
    (response) =>
      response.request().method() === 'DELETE' && new URL(response.url()).pathname === '/api/v1/admin/session',
  );
  await page.getByRole('button', { name: 'Sign out' }).click();
  const signOutAnswer = await signedOut;
  expect(signOutAnswer.status()).toBe(200);
  await expect(page.getByRole('status').filter({ hasText: 'You are signed out.' })).toBeVisible();
  expect(errors).toEqual([]);
  expect(await violations()).toEqual([]);

  await page.goto('about:blank');
  await openApp(page, link);
  await page.getByRole('button', { name: 'Create passkey' }).click();
  await expect(page.getByRole('alert')).toHaveText(/The link was already used/u);
});

test('the admin page of a server without a passkey tells how to set it up', async ({ newPage }) => {
  const page = await newPage();
  const errors = watchConsoleErrors(page);
  const violations = await watchViolations(page);
  const response = await openApp(page, `${POW_BASE_URL}/admin`);
  expect(response?.headers()['x-robots-tag']).toContain('noindex');
  await expect(page.getByRole('heading', { level: 1, name: 'The admin area is not set up yet' })).toBeVisible();
  await expect(page.locator('main code')).toContainText([
    'nylzo admin-enroll',
    'docker compose exec -T nylzo /ko-app/nylzo admin-enroll',
  ]);
  await expect(page.getByRole('button', { name: 'Sign in with passkey' })).toHaveCount(0);
  expect(errors).toEqual([]);
  expect(await violations()).toEqual([]);
});
