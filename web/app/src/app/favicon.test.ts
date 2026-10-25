import { readFileSync } from 'node:fs';
import { expect, it } from 'vitest';

const read = (path: string) => readFileSync(new URL(path, import.meta.url));

it('the favicon is the dark brand icon on navy from assets/brand', () => {
  expect(read('../../icons/favicon.svg')).toEqual(read('../../../../assets/brand/dark/nylzo-icon-dark-background.svg'));
});
