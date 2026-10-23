import { ApiError, createApi } from '../../app/src/lib/api.ts';
import { POW_BASE_URL, createViaApi, createViaUi, expect, expectRevealed, openViaUi, test } from './helpers.ts';
import { recordViolations } from './violations.ts';

test('the client solves the proof of work the instance asks for', async ({ browser }) => {
  const api = createApi({ base: POW_BASE_URL });
  while (!(await api.instance()).pow.required) {
    try {
      await createViaApi({ base: POW_BASE_URL });
    } catch (error) {
      if (!(error instanceof ApiError && error.kind === 'pow')) {
        throw error;
      }
    }
  }
  const sender = await (await browser.newContext()).newPage();
  const violations = await recordViolations(sender);
  const created = sender.waitForRequest((request) => request.url().endsWith('/api/v1/messages') && request.method() === 'POST');
  const link = await createViaUi(sender, { text: 'pow' }, POW_BASE_URL);
  expect((await created).headers()['x-nylzo-pow']).toMatch(/^[A-Za-z0-9_-]{48}\.[0-9a-f]{16}$/);
  expect(await violations()).toEqual([]);
  const recipient = await (await browser.newContext()).newPage();
  await openViaUi(recipient, link);
  await expectRevealed(recipient);
  await expect(recipient.locator('pre')).toHaveText('pow');
});
