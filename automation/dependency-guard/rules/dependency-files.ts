import type { FileRule } from './file-review.ts';
import { bunLock, goChecksums, miseLock, openTofuLock, pnpmLock } from './lockfiles.ts';
import { binaryAssetManifest, hostToolchain, packageManifest } from './manifests.ts';
import {
  composeFile,
  goModule,
  miseToolVersions,
  nodeVersion,
  openTofuVersions,
  releaseBaseImage,
  runtimeTest,
} from './pins.ts';
import { workflow } from './workflows.ts';

export const binaryAssetsPath = 'tools/dependencies/assets.json';
export const hostToolchainPath = 'deploy/hetzner-flatcar/toolchain.json';

const rules: readonly (readonly [RegExp, FileRule])[] = [
  [/^(?:deploy\/caddy\/)?go\.mod$/u, goModule],
  [/^(?:deploy\/caddy\/)?go\.sum$/u, goChecksums],
  [/^(?:automation|web(?:\/[^/]+)?)\/package\.json$/u, packageManifest],
  [
    /^web\/bun\.lock$/u,
    bunLock(['@nylzo/core@workspace:core', '@nylzo/web@workspace:app', '@nylzo/e2e@workspace:e2e']),
  ],
  [/^automation\/bun\.lock$/u, bunLock([])],
  [/^web\/pnpm-lock\.yaml$/u, pnpmLock(['../core'])],
  [/^automation\/pnpm-lock\.yaml$/u, pnpmLock([])],
  [/^mise\.toml$/u, miseToolVersions],
  [/^mise\.lock$/u, miseLock],
  [/^web\/\.node-version$/u, nodeVersion],
  [/^tools\/dependencies\/assets\.json$/u, binaryAssetManifest],
  [/^deploy\/hetzner-flatcar\/toolchain\.json$/u, hostToolchain],
  [/^deploy\/hetzner-flatcar\/infrastructure\/(?:dns|image|server)\/main\.tf$/u, openTofuVersions],
  [/^deploy\/hetzner-flatcar\/infrastructure\/(?:dns|image|server)\/\.terraform\.lock\.hcl$/u, openTofuLock],
  [/^\.goreleaser\.yaml$/u, releaseBaseImage],
  [/^deploy\/compose\/compose\.yaml$/u, composeFile],
  [/^tools\/deploy\/runtime\.test\.mts$/u, runtimeTest],
  [/^\.github\/workflows\/[^/]+\.ya?ml$/u, workflow],
  [/^\.github\/actions\/[^/]+\/action\.ya?ml$/u, workflow],
];

export function ruleFor(path: string): FileRule | undefined {
  return rules.find(([pattern]) => pattern.test(path))?.[1];
}
