import type { FileRule } from './file-review.ts';

import { goChecksums, miseLock, openTofuLock, pnpmLock } from './lockfiles.ts';
import { binaryAssetManifest, hostToolchain, packageManifest } from './manifests.ts';
import {
  composeFile,
  deployTask,
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
  [/^go\.mod$/, goModule],
  [/^go\.sum$/, goChecksums],
  [/^(?:automation|web(?:\/[^/]+)?)\/package\.json$/, packageManifest],
  [/^web\/pnpm-lock\.yaml$/, pnpmLock(['../core'])],
  [/^automation\/pnpm-lock\.yaml$/, pnpmLock([])],
  [/^mise\.toml$/, miseToolVersions],
  [/^mise\.lock$/, miseLock],
  [/^web\/\.node-version$/, nodeVersion],
  [/^tools\/dependencies\/assets\.json$/, binaryAssetManifest],
  [/^deploy\/hetzner-flatcar\/toolchain\.json$/, hostToolchain],
  [/^deploy\/hetzner-flatcar\/infrastructure\/(?:dns|image|server)\/main\.tf$/, openTofuVersions],
  [/^deploy\/hetzner-flatcar\/infrastructure\/(?:dns|image|server)\/\.terraform\.lock\.hcl$/, openTofuLock],
  [/^mise-tasks\/deploy$/, deployTask],
  [/^\.goreleaser\.yaml$/, releaseBaseImage],
  [/^deploy\/compose\/compose\.yaml$/, composeFile],
  [/^tools\/deploy\/runtime\.test\.mts$/, runtimeTest],
  [/^\.github\/workflows\/[^/]+\.ya?ml$/, workflow],
];

export function ruleFor(path: string): FileRule | undefined {
  return rules.find(([pattern]) => pattern.test(path))?.[1];
}
