import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

import { checkPatchBinding } from './deepmerge-patch.ts';

const web = new URL('../../web/', import.meta.url);

test('deepmerge remediation requires the exact patch and every locked consumer', async () => {
  const patch = await readFile(new URL('patches/deepmerge@4.3.1.patch', web));
  const workspace = await readFile(new URL('package.json', web), 'utf8');
  const lockfile = await readFile(new URL('bun.lock', web), 'utf8');
  checkPatchBinding(patch, workspace, lockfile);
  checkPatchBinding(patch, workspace.replaceAll('\n', '\r\n'), lockfile.replaceAll('\n', '\r\n'));
  assert.throws(() => checkPatchBinding(Buffer.concat([patch, Buffer.from('\n')]), workspace, lockfile));
  assert.throws(() => checkPatchBinding(patch, workspace.replace('patchedDependencies', 'removedPatches'), lockfile));
  assert.throws(() => checkPatchBinding(patch, workspace, lockfile.replace('patchedDependencies', 'removedPatches')));
  assert.throws(() =>
    checkPatchBinding(patch, workspace, lockfile.replace('"deepmerge@4.3.1",', '"deepmerge@4.3.2",')),
  );
  assert.throws(() =>
    checkPatchBinding(
      patch,
      workspace,
      lockfile.replace('"deepmerge@4.3.1", ""', '"deepmerge@4.3.1", "https://evil.test"'),
    ),
  );
});
