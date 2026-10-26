import { describe, expect, it } from 'vitest';

import { type BuildInfo, buildSourceHref } from './build-info.ts';
import { REPOSITORY_URL } from './shell/repository.ts';

const BUILD: BuildInfo = {
  version: '0.1.0',
  commit: 'a123456789abcdef0123456789abcdef01234567',
  date: '2026-10-26T12:00:00Z',
  dirty: false,
};

describe('build source links', () => {
  it('links a stable release to its canonical tag', () => {
    expect(buildSourceHref(BUILD)).toBe(`${REPOSITORY_URL}/releases/tag/v0.1.0`);
  });

  it.each([
    { version: '0.0.0-dev.a123456789ab', dirty: false },
    { version: '0.1.0-snapshot', dirty: false },
    { version: '0.1.0', dirty: true },
    { version: '01.1.0', dirty: false },
  ])('links development build %j to its source commit', (build) => {
    expect(buildSourceHref({ ...BUILD, ...build })).toBe(`${REPOSITORY_URL}/commit/${BUILD.commit}`);
  });

  it.each(['unknown', '../main', 'abc1234?tab=readme', ''])('does not make a source link from %j', (commit) => {
    expect(buildSourceHref({ ...BUILD, version: 'development', commit })).toBeUndefined();
  });
});
