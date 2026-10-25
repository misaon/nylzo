import { describe, expect, it } from 'vitest';

import { vectors } from '../../test/vectors.ts';
import { WeakPassphraseError } from '../errors.ts';
import { assertStrongPassphrase } from './kdf.ts';

describe('AK9: passphrase strength vectors', () => {
  const accepted = vectors.passphrase_strength.filter((vector) => vector.accepted);
  const rejected = vectors.passphrase_strength.filter((vector) => !vector.accepted);

  it.each(accepted)('$name is accepted', (vector) => {
    expect(() => {
      assertStrongPassphrase(vector.passphrase);
    }).not.toThrow();
  });

  it.each(rejected)('$name is rejected', (vector) => {
    expect(() => {
      assertStrongPassphrase(vector.passphrase);
    }).toThrow(WeakPassphraseError);
  });
});
