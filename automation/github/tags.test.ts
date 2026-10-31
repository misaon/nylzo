import assert from 'node:assert/strict';
import { test } from 'node:test';

import { type GitObject, peelTag, type TagReader } from './tags.ts';

const repository = { owner: 'actions', repo: 'checkout' };
const commit: GitObject = { sha: 'c'.repeat(40), type: 'commit' };

function annotated(digit: string): GitObject {
  return { sha: digit.repeat(40), type: 'tag' };
}

function tagReader(objects: ReadonlyMap<string, unknown>): TagReader {
  return {
    rest: {
      git: {
        getTag: ({ owner, repo, tag_sha }) => {
          assert.deepEqual({ owner, repo }, repository);
          return objects.has(tag_sha)
            ? Promise.resolve({ data: { object: objects.get(tag_sha) } })
            : Promise.reject(new Error(`Unknown tag ${tag_sha}`));
        },
      },
    },
  };
}

await test('a tag resolves to the commit it names, through at most the allowed nesting', async () => {
  const chain = tagReader(
    new Map([
      [annotated('1').sha, annotated('2')],
      [annotated('2').sha, annotated('3')],
      [annotated('3').sha, annotated('4')],
      [annotated('4').sha, commit],
    ]),
  );
  assert.deepEqual(await peelTag(chain, repository, commit), commit);
  assert.deepEqual(await peelTag(chain, repository, annotated('4')), commit);
  assert.deepEqual(await peelTag(chain, repository, annotated('2')), commit);
  assert.deepEqual(await peelTag(chain, repository, annotated('1')), annotated('4'));
  assert.deepEqual(await peelTag(chain, repository, annotated('3'), 1), annotated('4'));
});

await test('a tag object without a full commit hash is rejected', async () => {
  const malformed = tagReader(new Map([[annotated('1').sha, { sha: 'main', type: 'commit' }]]));
  await assert.rejects(peelTag(malformed, repository, annotated('1')));
  await assert.rejects(peelTag(malformed, repository, { sha: 'main', type: 'commit' }));
});
