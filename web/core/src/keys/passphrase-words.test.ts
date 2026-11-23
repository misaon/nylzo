import { describe, expect, it, vi } from 'vitest';

import { loadPassphraseWords } from './passphrase-words.ts';

vi.mock('./words/cs.ts', () => ({ WORDS: 'jedna dva tři' }));

describe('passphrase word lists', () => {
  it('refuse a list that does not hold 2048 words', async () => {
    await expect(loadPassphraseWords('cs')).rejects.toThrow('the cs word list does not hold 2048 words');
  });
});
