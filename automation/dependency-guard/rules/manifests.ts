import { isDeepStrictEqual } from 'node:util';

import { z } from 'zod';

import { emptyReview } from './file-review.ts';
import type { FileReview, FileRule, ValueKind } from './file-review.ts';

type JsonObject = Record<string, unknown>;

const dependencySections = ['dependencies', 'devDependencies', 'optionalDependencies', 'peerDependencies'];
const exactVersion = /^\d+\.\d+\.\d+$/u;
const packageManagerRelease = /^(?<manager>pnpm|bun)@(?<version>\d+\.\d+\.\d+)$/u;
const sha256 = /^[a-f\d]{64}$/u;
const versionPrefix = /^(?<prefix>jq-|v)?(?<version>.*)$/u;

function isObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function parseJson(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

function parseObject(path: string, text: string, review: FileReview): JsonObject | undefined {
  const value = parseJson(text);
  if (isObject(value)) {
    return value;
  }
  review.findings.push(`${path}: not a JSON object`);
  return undefined;
}

function sameKeys(left: Readonly<JsonObject>, right: Readonly<JsonObject>): boolean {
  const keys = Object.keys(left);
  return keys.length === Object.keys(right).length && keys.every((key) => Object.hasOwn(right, key));
}

export function revertOverrideUpdates(path: string, before: unknown, after: unknown, review: FileReview): unknown {
  if (!isObject(before) || !isObject(after)) {
    return after;
  }
  const reverted = { ...after };
  for (const [name, version] of Object.entries(after)) {
    const previous = before[name];
    if (
      typeof previous === 'string' &&
      typeof version === 'string' &&
      previous !== version &&
      exactVersion.test(previous) &&
      exactVersion.test(version)
    ) {
      review.updates.push({ after: version, before: previous, name: `${path} overrides.${name}` });
      reverted[name] = previous;
    }
  }
  return reverted;
}

export const packageManifest: FileRule = (path, before, after) => {
  const review = emptyReview();
  const old = parseObject(path, before, review);
  const next = parseObject(path, after, review);
  if (!old || !next) {
    return review;
  }
  for (const section of dependencySections) {
    const oldDependencies = old[section];
    const newDependencies = next[section];
    if (!isObject(oldDependencies) || !isObject(newDependencies)) {
      continue;
    }
    for (const [name, value] of Object.entries(oldDependencies)) {
      const updated = newDependencies[name];
      if (typeof value !== 'string' || typeof updated !== 'string' || value === updated) {
        continue;
      }
      const alias = /^npm:(?<target>(?:@[\w.-]+\/)?[\w.-]+)@(?<version>\d+\.\d+\.\d+)$/u;
      const fromAlias = alias.exec(value)?.groups;
      const toAlias = alias.exec(updated)?.groups;
      if ((fromAlias !== undefined || toAlias !== undefined) && fromAlias?.['target'] !== toAlias?.['target']) {
        review.findings.push(`${path}: dependency alias ${name} changed its package source`);
      }
      review.updates.push({
        after: toAlias?.['version'] ?? updated,
        before: fromAlias?.['version'] ?? value,
        name: `${path} ${section}.${name}`,
      });
      newDependencies[name] = value;
    }
  }
  if (Object.hasOwn(next, 'overrides')) {
    next['overrides'] = revertOverrideUpdates(path, old['overrides'], next['overrides'], review);
  }
  if (old['packageManager'] !== next['packageManager']) {
    const previous = packageManagerRelease.exec(String(old['packageManager']))?.groups;
    const updated = packageManagerRelease.exec(String(next['packageManager']))?.groups;
    const from = previous?.['version'];
    const to = updated?.['version'];
    if (from !== undefined && to !== undefined && previous?.['manager'] === updated?.['manager']) {
      const oldEngines = old['engines'];
      const nextEngines = next['engines'];
      if (
        previous?.['manager'] === 'bun' &&
        isObject(oldEngines) &&
        isObject(nextEngines) &&
        oldEngines['bun'] === from &&
        nextEngines['bun'] === to
      ) {
        review.updates.push({ after: to, before: from, name: `${path} engines.bun` });
        nextEngines['bun'] = oldEngines['bun'];
      }
      review.updates.push({ after: to, before: from, name: `${path} packageManager` });
      next['packageManager'] = old['packageManager'];
    }
  }
  if (!isDeepStrictEqual(next, old)) {
    review.findings.push(
      `${path}: only dependency versions may change, not the dependency list, scripts or other fields`,
    );
  }
  return review;
};

function compareTree(
  path: string,
  location: readonly string[],
  before: unknown,
  after: unknown,
  leaves: Readonly<Record<string, ValueKind>>,
  review: FileReview,
): void {
  const where = location.join('.') || 'the top level';
  if (isObject(before) && isObject(after)) {
    if (!sameKeys(before, after)) {
      review.findings.push(`${path}: ${where} gained or lost fields`);
      return;
    }
    for (const [key, value] of Object.entries(before)) {
      compareTree(path, [...location, key], value, after[key], leaves, review);
    }
    return;
  }
  if (isDeepStrictEqual(before, after)) {
    return;
  }
  const kind = leaves[location.at(-1) ?? ''];
  if (typeof before !== 'string' || typeof after !== 'string' || kind === undefined || kind === 'fixed') {
    review.findings.push(`${path}: ${where} cannot change automatically`);
  } else if (kind === 'opaque') {
    if (!sha256.test(after)) {
      review.findings.push(`${path}: ${where} is not a SHA-256 checksum`);
    }
  } else {
    const old = versionPrefix.exec(before)?.groups;
    const next = versionPrefix.exec(after)?.groups;
    if (old?.['prefix'] === next?.['prefix']) {
      review.updates.push({
        after: next?.['version'] ?? after,
        before: old?.['version'] ?? before,
        name: `${path} ${where}`,
      });
    } else {
      review.findings.push(`${path}: ${where} changed its version format`);
    }
  }
}

function manifestRule(leaves: Readonly<Record<string, ValueKind>>): FileRule {
  return (path, before, after) => {
    const review = emptyReview();
    const old = parseObject(path, before, review);
    const next = parseObject(path, after, review);
    if (old && next) {
      compareTree(path, [], old, next, leaves, review);
    }
    return review;
  };
}

const binaryAsset = z.strictObject({ package: z.string(), sha256: z.string(), version: z.string() });
const binaryAssets = z.record(z.string(), binaryAsset);

type BinaryAsset = z.infer<typeof binaryAsset>;

function plainVersion(version: string): string {
  return version.replace(/^(?:jq-|v)/u, '');
}

function releaseAssetName(key: string, asset: BinaryAsset): string | undefined {
  const { architecture = '', tool = '' } = /^(?<tool>[a-z]+)-(?<architecture>amd64|arm64)$/u.exec(key)?.groups ?? {};
  const version = plainVersion(asset.version);
  const amd64 = architecture === 'amd64';
  switch (tool) {
    case 'codecov': {
      return `codecovcli_linux${amd64 ? '' : '_arm64'}`;
    }
    case 'cosign': {
      return `cosign-linux-${architecture}`;
    }
    case 'gh': {
      return `gh_${version}_linux_${architecture}.tar.gz`;
    }
    case 'ignition': {
      return `ignition-validate-${amd64 ? 'x86_64' : 'aarch64'}-linux`;
    }
    case 'jq': {
      return `jq-linux-${architecture}`;
    }
    case 'mise': {
      return `mise-v${version}-linux-${amd64 ? 'x64' : 'arm64'}.tar.gz`;
    }
    case 'syft': {
      return `syft_${version}_linux_${architecture}.tar.gz`;
    }
    default: {
      return undefined;
    }
  }
}

const assetManifest = manifestRule({ sha256: 'opaque', version: 'version' });

export const binaryAssetManifest: FileRule = (path, before, after) => {
  const review = assetManifest(path, before, after);
  if (review.findings.length > 0) {
    return review;
  }
  const old = binaryAssets.safeParse(parseJson(before));
  const next = binaryAssets.safeParse(parseJson(after));
  if (!old.success || !next.success) {
    review.findings.push(`${path}: every asset needs exactly a package, a version and a sha256`);
    return review;
  }
  for (const [key, asset] of Object.entries(next.data)) {
    if (isDeepStrictEqual(old.data[key], asset)) {
      continue;
    }
    const name = releaseAssetName(key, asset);
    if (name === undefined) {
      review.findings.push(`${path}: no upstream release asset is known for ${key}`);
      continue;
    }
    review.claims.push({
      asset: name,
      digest: `sha256:${asset.sha256}`,
      kind: 'release-asset',
      repository: asset.package,
      tag: asset.version,
    });
  }
  return review;
};

export const hostToolchain = manifestRule({
  amd64: 'opaque',
  arm64: 'opaque',
  caddy: 'version',
  flatcar: 'version',
  opentofu: 'version',
  version: 'version',
});

const toolchainEntry = z.object({ amd64: z.string(), arm64: z.string(), version: z.string() });
const toolchain = z.object({ cosign: toolchainEntry, gh: toolchainEntry, jq: toolchainEntry });

export function toolchainMismatches(toolchainText: string, assetsText: string): string[] {
  const hosts = toolchain.safeParse(parseJson(toolchainText));
  const assets = binaryAssets.safeParse(parseJson(assetsText));
  if (!hosts.success || !assets.success) {
    return ['The host toolchain or the binary asset list has an unexpected shape'];
  }
  const mismatches: string[] = [];
  for (const tool of ['cosign', 'gh', 'jq'] as const) {
    const host = hosts.data[tool];
    for (const architecture of ['amd64', 'arm64'] as const) {
      const asset = assets.data[`${tool}-${architecture}`];
      if (asset === undefined || plainVersion(asset.version) !== host.version || asset.sha256 !== host[architecture]) {
        mismatches.push(`The host toolchain ${tool} ${architecture} differs from the verified binary asset list`);
      }
    }
  }
  return mismatches;
}
