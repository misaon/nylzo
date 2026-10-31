import assert from 'node:assert/strict';
import { test } from 'node:test';

import type { FakeRepository, FileChange } from './fake-source.ts';
import type { Commit, Review } from './source.ts';

import {
  appAccount,
  appCommit,
  baseCommit,
  bot,
  codeOwnersFile,
  dependencyUpdate,
  fakeSource,
  gitHubWebFlow,
  headCommit,
  owner,
  stranger,
} from './fake-source.ts';
import { decide } from './guard.ts';

const oldAction = '1'.repeat(40);
const newAction = '2'.repeat(40);
const otherBase = 'd'.repeat(40);
const oldDigest = 'a'.repeat(64);
const newDigest = 'e'.repeat(64);

const workflow = (commit: string, version: string): string =>
  `jobs:\n  lint:\n    steps:\n      - uses: jdx/mise-action@${commit} # ${version}\n        with:\n          cache: false\n`;

const assets = (version: string, digest: string): string =>
  `{\n  "mise-amd64": {"package": "jdx/mise", "version": "${version}", "sha256": "${digest}"}\n}\n`;

const hostTools = ['cosign', 'gh', 'jq'] as const;

const toolchain = (jqDigest: string): string =>
  JSON.stringify(
    Object.fromEntries(
      hostTools.map((tool) => [
        tool,
        { amd64: tool === 'jq' ? jqDigest : oldDigest, arm64: oldDigest, version: '1.8.2' },
      ]),
    ),
  );

const hostAsset = (tool: string): object => ({ package: `example/${tool}`, sha256: oldDigest, version: 'v1.8.2' });

const hostAssets = JSON.stringify({
  'cosign-amd64': hostAsset('cosign'),
  'cosign-arm64': hostAsset('cosign'),
  'gh-amd64': hostAsset('gh'),
  'gh-arm64': hostAsset('gh'),
  'jq-amd64': hostAsset('jq'),
  'jq-arm64': hostAsset('jq'),
});

const safeChanges: readonly FileChange[] = [
  { after: workflow(newAction, 'v5.2.5'), before: workflow(oldAction, 'v5.2.4'), path: '.github/workflows/ci.yml' },
  {
    after: assets('v2026.10.21', newDigest),
    before: assets('v2026.10.19', oldDigest),
    path: 'tools/dependencies/assets.json',
  },
];

const upstream = {
  releaseAssets: { 'jdx/mise@v2026.10.21/mise-v2026.10.21-linux-x64.tar.gz': `sha256:${newDigest}` },
  tags: { 'jdx/mise-action@v5.2.4': oldAction, 'jdx/mise-action@v5.2.5': newAction },
};

function safeUpdate(overrides: Partial<FakeRepository> = {}): FakeRepository {
  return dependencyUpdate(safeChanges, { ...upstream, ...overrides });
}

function withPull(overrides: Partial<FakeRepository['pull']>, repository = safeUpdate()): FakeRepository {
  return { ...repository, pull: { ...repository.pull, ...overrides } };
}

function review(reviewer: Review['reviewer'], state: string, commit: string | null = headCommit): Review {
  return { commit, reviewer, state };
}

const secondOwner = { id: 2, login: 'second-owner', type: 'User' };
const twoOwners = '* @misaon @second-owner\n';
const twoOwnerSnapshots = Object.fromEntries(
  Object.entries(safeUpdate().snapshots).map(([commit, files]) => [
    commit,
    { ...files, '.github/CODEOWNERS': twoOwners },
  ]),
);
const ownerCommit: Commit = { author: owner, committer: owner, parents: [baseCommit], sha: headCommit, verified: true };
const strangerCommit = appCommit({ author: stranger, committer: stranger });
const approval = review(owner, 'APPROVED');
const tooManyFiles: readonly FileChange[] = Array.from({ length: 81 }, (_, index) => ({
  after: workflow(newAction, 'v5.2.5'),
  before: workflow(oldAction, 'v5.2.4'),
  path: `.github/workflows/job-${index}.yml`,
}));
const majorUpdate: readonly FileChange[] = [
  { after: workflow(newAction, 'v6.0.0'), before: workflow(oldAction, 'v5.2.4'), path: '.github/workflows/ci.yml' },
];
const majorUpstream = { tags: { 'jdx/mise-action@v6.0.0': newAction } };

interface Scenario {
  readonly head?: string;
  readonly name: string;
  readonly passed: boolean;
  readonly reason: RegExp;
  readonly repository: FakeRepository;
}

const scenarios: readonly Scenario[] = [
  {
    name: 'a verified, safe App update',
    passed: true,
    reason: /verified, safe dependency update/,
    repository: safeUpdate(),
  },
  {
    name: 'a safe App update to a maintenance branch',
    passed: true,
    reason: /verified, safe/,
    repository: withPull({ baseBranch: 'release/v0.2' }, safeUpdate({ branches: { 'release/v0.2': baseCommit } })),
  },
  {
    name: 'an Action SHA that belongs to another tag',
    passed: false,
    reason: /is not the commit of its tag v5\.2\.5/,
    repository: dependencyUpdate(
      [
        {
          after: workflow(oldAction, 'v5.2.5'),
          before: workflow(oldAction, 'v5.2.4'),
          path: '.github/workflows/ci.yml',
        },
      ],
      upstream,
    ),
  },
  {
    name: 'an Action tag missing upstream',
    passed: false,
    reason: /tag not found/,
    repository: safeUpdate({ tags: {} }),
  },
  {
    name: 'a checksum that differs from the upstream digest',
    passed: false,
    reason: /is not the release asset digest/,
    repository: safeUpdate({
      releaseAssets: { 'jdx/mise@v2026.10.21/mise-v2026.10.21-linux-x64.tar.gz': `sha256:${'9'.repeat(64)}` },
    }),
  },
  {
    name: 'an unsigned App commit',
    passed: false,
    reason: /no verified signature/,
    repository: safeUpdate({ commits: [appCommit({ verified: false })] }),
  },
  {
    name: 'an App commit pushed by someone else',
    passed: false,
    reason: /not authored and committed by the automation App/,
    repository: safeUpdate({ commits: [appCommit({ committer: stranger })] }),
  },
  {
    name: 'a non-App commit in an App pull request without approval',
    passed: false,
    reason: /approve .* and then re-run/,
    repository: safeUpdate({ commits: [strangerCommit] }),
  },
  {
    name: 'a non-App commit in an App pull request with approval on the head',
    passed: true,
    reason: /Code owner @misaon approved/,
    repository: safeUpdate({ commits: [strangerCommit], reviews: [approval] }),
  },
  {
    name: 'an App update on an outdated base',
    passed: false,
    reason: /Rebase required/,
    repository: safeUpdate({
      branches: { main: otherBase },
      snapshots: { ...safeUpdate().snapshots, [otherBase]: { '.github/CODEOWNERS': codeOwnersFile } },
    }),
  },
  { name: 'a draft App update', passed: false, reason: /draft/, repository: withPull({ draft: true }) },
  {
    name: 'an App update from a fork',
    passed: false,
    reason: /comes from attacker\/nylzo/,
    repository: withPull({ headRepository: 'attacker/nylzo' }),
  },
  {
    name: 'an App update to a legacy release branch',
    passed: false,
    reason: /targets release\/v0\.1\.12/,
    repository: withPull(
      { baseBranch: 'release/v0.1.12' },
      safeUpdate({ branches: { 'release/v0.1.12': baseCommit } }),
    ),
  },
  {
    name: 'an App update with two commits',
    passed: false,
    reason: /has 2 commits/,
    repository: withPull({ commitCount: 2 }, safeUpdate({ commits: [appCommit({ sha: otherBase }), appCommit()] })),
  },
  {
    name: 'an App update of more than 80 files',
    passed: false,
    reason: /more than 80 files/,
    repository: dependencyUpdate(tooManyFiles, upstream),
  },
  {
    name: 'an App update that adds a file',
    passed: false,
    reason: /Adds web\/app\/src\/evil\.ts/,
    repository: dependencyUpdate([...safeChanges, { after: 'export {}', path: 'web/app/src/evil.ts' }], upstream),
  },
  {
    name: 'an App update that removes a file',
    passed: false,
    reason: /Removes SECURITY\.md/,
    repository: dependencyUpdate([...safeChanges, { before: '# Security', path: 'SECURITY.md' }], upstream),
  },
  {
    name: 'an App update that makes a file executable',
    passed: false,
    reason: /type or mode of mise\.toml/,
    repository: dependencyUpdate(
      [
        ...safeChanges,
        { after: { content: 'node = "24.22.1"\n', mode: '100755' }, before: 'node = "24.22.1"\n', path: 'mise.toml' },
      ],
      upstream,
    ),
  },
  {
    name: 'an App update that changes source code',
    passed: false,
    reason: /internal\/server\/server\.go is not a dependency file/,
    repository: dependencyUpdate(
      [...safeChanges, { after: 'package b', before: 'package a', path: 'internal/server/server.go' }],
      upstream,
    ),
  },
  {
    name: 'an App update whose host toolchain differs from the verified assets',
    passed: false,
    reason: /host toolchain jq amd64 differs/,
    repository: dependencyUpdate(
      [
        { after: toolchain(newDigest), before: toolchain(oldDigest), path: 'deploy/hetzner-flatcar/toolchain.json' },
        { after: hostAssets, before: hostAssets, path: 'tools/dependencies/assets.json' },
      ],
      upstream,
    ),
  },
  {
    name: 'an App update whose host toolchain matches the verified assets',
    passed: true,
    reason: /verified, safe/,
    repository: dependencyUpdate(
      [
        { after: toolchain(oldDigest), before: toolchain(newDigest), path: 'deploy/hetzner-flatcar/toolchain.json' },
        { after: hostAssets, before: hostAssets, path: 'tools/dependencies/assets.json' },
      ],
      upstream,
    ),
  },
  {
    name: 'a major App update without approval',
    passed: false,
    reason: /changes the major version/,
    repository: dependencyUpdate(majorUpdate, majorUpstream),
  },
  {
    name: 'a major App update approved on the head',
    passed: true,
    reason: /Code owner @misaon approved/,
    repository: dependencyUpdate(majorUpdate, { ...majorUpstream, reviews: [approval] }),
  },
  {
    name: 'a major App update approved on an older head',
    passed: false,
    reason: /approve/,
    repository: dependencyUpdate(majorUpdate, { ...majorUpstream, reviews: [review(owner, 'APPROVED', otherBase)] }),
  },
  {
    name: 'a major App update whose approval was dismissed',
    passed: false,
    reason: /approve/,
    repository: dependencyUpdate(majorUpdate, { ...majorUpstream, reviews: [approval, review(owner, 'DISMISSED')] }),
  },
  {
    name: 'a major App update whose approval was followed by a comment',
    passed: true,
    reason: /approved/,
    repository: dependencyUpdate(majorUpdate, { ...majorUpstream, reviews: [approval, review(owner, 'COMMENTED')] }),
  },
  {
    name: 'a major App update approved by someone who is not a code owner',
    passed: false,
    reason: /approve/,
    repository: dependencyUpdate(majorUpdate, { ...majorUpstream, reviews: [review(stranger, 'APPROVED')] }),
  },
  {
    name: 'a pull request by a code owner',
    passed: true,
    reason: /Authored by code owner @misaon/,
    repository: withPull({ author: owner }, safeUpdate({ commits: [ownerCommit] })),
  },
  {
    name: 'a pull request by a code owner with a commit pushed by the App',
    passed: false,
    reason: /not a signed commit of a code owner/,
    repository: withPull({ author: owner, commitCount: 2 }, safeUpdate({ commits: [ownerCommit, appCommit()] })),
  },
  {
    name: 'a pull request by a code owner with an unsigned commit',
    passed: false,
    reason: /not a signed commit of a code owner/,
    repository: withPull({ author: owner }, safeUpdate({ commits: [{ ...ownerCommit, verified: false }] })),
  },
  {
    name: 'a pull request by a code owner with a commit made on GitHub',
    passed: true,
    reason: /Authored by code owner/,
    repository: withPull({ author: owner }, safeUpdate({ commits: [{ ...ownerCommit, committer: gitHubWebFlow }] })),
  },
  {
    name: 'a pull request by another author without approval',
    passed: false,
    reason: /@octocat is neither a code owner nor the automation App/,
    repository: withPull({ author: stranger }, safeUpdate({ commits: [strangerCommit] })),
  },
  {
    name: 'a pull request by another author with approval on the head',
    passed: true,
    reason: /Code owner @misaon approved/,
    repository: withPull({ author: stranger }, safeUpdate({ commits: [strangerCommit], reviews: [approval] })),
  },
  {
    name: 'a pull request by another author whose approval faces requested changes',
    passed: false,
    reason: /approve/,
    repository: withPull(
      { author: stranger },
      safeUpdate({
        commits: [strangerCommit],
        reviews: [review(owner, 'CHANGES_REQUESTED'), approval, review(owner, 'CHANGES_REQUESTED')],
      }),
    ),
  },
  {
    name: 'a pull request approved by one code owner while another requested changes',
    passed: false,
    reason: /approve/,
    repository: withPull(
      { author: stranger },
      safeUpdate({
        commits: [strangerCommit],
        reviews: [review(secondOwner, 'CHANGES_REQUESTED'), approval],
        snapshots: twoOwnerSnapshots,
      }),
    ),
  },
  {
    name: 'a pull request that moved after the event',
    head: otherBase,
    passed: false,
    reason: /the run for the new head decides/,
    repository: safeUpdate(),
  },
  {
    name: 'an App account impersonated by a user',
    passed: false,
    reason: /neither a code owner nor the automation App/,
    repository: withPull({ author: { ...appAccount, id: 1 } }),
  },
];

for (const scenario of scenarios) {
  void test(`${scenario.name} ${scenario.passed ? 'passes' : 'fails'}`, async () => {
    const verdict = await decide(fakeSource(scenario.repository), bot, {
      head: scenario.head ?? headCommit,
      number: scenario.repository.pull.number,
    });
    assert.equal(verdict.passed, scenario.passed, verdict.reasons.join('\n'));
    assert.ok(
      verdict.reasons.some((reason) => scenario.reason.test(reason)),
      verdict.reasons.join('\n'),
    );
  });
}
