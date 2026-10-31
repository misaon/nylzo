import assert from 'node:assert/strict';
import { test } from 'node:test';

import { actionPins } from './action-pins.ts';

test('action pins are read only from complete uses lines', () => {
  const workflow = [
    `      - uses: github/codeql-action/init@${'1'.repeat(40)} # v4.31.2`,
    `        uses: actions/checkout@${'2'.repeat(40)} # v7.0.1`,
    `      # - uses: attacker/action@${'3'.repeat(40)} # v1.0.0`,
    `      - uses: actions/cache@${'4'.repeat(40)} # v5.0.0-rc.1`,
  ].join('\n');
  assert.deepEqual(actionPins(workflow), [
    { action: 'github/codeql-action', commit: '1'.repeat(40), version: 'v4.31.2' },
    { action: 'actions/checkout', commit: '2'.repeat(40), version: 'v7.0.1' },
  ]);
});
