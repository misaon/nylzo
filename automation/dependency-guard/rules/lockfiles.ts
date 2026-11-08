import { isDeepStrictEqual } from 'node:util';

import { objectAt } from '../../source-dependency-gate/json-values.ts';
import { compareSnapshots, emptyReview, placeholder, quote, snapshot } from './file-review.ts';
import type { DependencyValue, FileReview, FileRule, Snapshot } from './file-review.ts';
import { packageManifest } from './manifests.ts';

const deprecation = /^ +deprecated: /u;
const integrityResolution = /^ +resolution: \{integrity: sha(?:256|384|512)-[\d+/A-Za-z]+=*\}$/u;
const escapedOrAliased = /\\|[&*][\w-]/u;
const customSourceKey = /(?:^|[\s,{])["']?(?:commit|repo|tarball)["']?\s*:/u;
const externalSource = /\b(?:[a-z][\d+.a-z-]*:\/\/|git\+|git@|file:|link:|github:|gitlab:|bitbucket:)/iu;
const workspaceLink = /\blink:(?<target>[^\s"',}]*)/gu;

function documentHeaders(lockfile: string): string[] {
  return lockfile.split(/^---$/mu).map((document) => document.split(/^(?:importers|packages):$/mu, 1)[0] ?? '');
}

function lockfileLineProblem(line: string, allowedLinks: readonly string[]): string | undefined {
  if (escapedOrAliased.test(line)) {
    return 'escaped or aliased value';
  }
  if (customSourceKey.test(line)) {
    return 'custom package source';
  }
  const withoutWorkspaceLinks = line.replaceAll(workspaceLink, (link, target: string) =>
    allowedLinks.includes(target) ? '' : link,
  );
  if (externalSource.test(withoutWorkspaceLinks)) {
    return 'download source outside the registry';
  }
  if (line.includes('resolution') && !integrityResolution.test(line)) {
    return 'resolution without registry integrity';
  }
  return undefined;
}

export function pnpmLock(allowedLinks: readonly string[]): FileRule {
  return (path, before, after) => {
    const review = emptyReview();
    if (documentHeaders(before).join('\n---\n') !== documentHeaders(after).join('\n---\n')) {
      review.findings.push(`${path}: lockfile settings, overrides or patches changed`);
    }
    const reviewedDeprecations = new Set(before.split('\n').filter((line) => deprecation.test(line)));
    for (const [index, line] of after.split('\n').entries()) {
      if (deprecation.test(line) && reviewedDeprecations.has(line)) {
        continue;
      }
      const problem = lockfileLineProblem(line, allowedLinks);
      if (problem) {
        review.findings.push(`${path}:${index + 1}: ${problem}: ${quote(line)}`);
      }
    }
    if (after.split('\n').every((line) => !integrityResolution.test(line))) {
      review.findings.push(`${path}: no package carries a registry integrity`);
    }
    return review;
  };
}

// Bun's text lockfile is JSON with trailing commas. Never evaluate it as JavaScript.
export function bunLock(allowedWorkspaces: readonly string[]): FileRule {
  return (path, before, after) => {
    const review = emptyReview();
    try {
      const previous = lockObject(Bun.JSON5.parse(before));
      const next = lockObject(Bun.JSON5.parse(after));
      const { packages: oldPackages, workspaces: oldWorkspaces, ...oldPolicy } = previous;
      const { packages, workspaces, ...policy } = next;
      if (!isDeepStrictEqual(oldPolicy, policy)) {
        review.findings.push(`${path}: lockfile settings, overrides, trust or patches changed`);
      }
      if (![2, 3].includes(Number(next['lockfileVersion'])) || next['configVersion'] !== 1) {
        review.findings.push(`${path}: unsupported Bun lockfile format`);
      }
      const oldRoots = lockObject(oldWorkspaces);
      const roots = lockObject(workspaces);
      if (!isDeepStrictEqual(Object.keys(oldRoots).toSorted(), Object.keys(roots).toSorted())) {
        review.findings.push(`${path}: workspace list changed`);
      }
      for (const [name, manifest] of Object.entries(roots)) {
        const result = packageManifest(
          `${path} workspace ${name}`,
          JSON.stringify(oldRoots[name]),
          JSON.stringify(manifest),
        );
        review.findings.push(...result.findings);
        review.updates.push(...result.updates);
      }
      const entries = Object.entries(lockObject(packages));
      if (entries.length === 0 || Object.keys(lockObject(oldPackages)).length === 0) {
        review.findings.push(`${path}: empty dependency inventory`);
      }
      for (const [name, entry] of entries) {
        if (!Array.isArray(entry) || typeof entry[0] !== 'string') {
          review.findings.push(`${path}: invalid package ${name}`);
          continue;
        }
        const resolved = entry[0];
        if (resolved.includes('@workspace:')) {
          if (entry.length !== 1 || !allowedWorkspaces.includes(resolved)) {
            review.findings.push(`${path}: unapproved workspace ${resolved}`);
          }
          continue;
        }
        const match = /^(?:@[\w.-]+\/)?[\w.-]+@\d+\.\d+\.\d+(?:-[\da-z.-]+)?(?:\+[\da-z.-]+)?$/iu.exec(resolved);
        if (
          !match ||
          entry.length !== 4 ||
          entry[1] !== '' ||
          typeof entry[3] !== 'string' ||
          !/^sha(?:256|384|512)-[\d+/A-Za-z]+=*$/u.test(entry[3])
        ) {
          review.findings.push(`${path}: ${name} must resolve to a registry package with integrity`);
        }
        if (externalSource.test(JSON.stringify(entry[2]))) {
          review.findings.push(`${path}: ${name} has a dependency outside the registry`);
        }
      }
    } catch {
      review.findings.push(`${path}: invalid Bun lockfile`);
    }
    return review;
  };
}

function lockObject(value: unknown): Readonly<Record<string, unknown>> {
  return objectAt(value, 'Bun lockfile');
}

const goChecksumLine = /^[\w.~+!/-]+ v\S+ h1:[\d+/A-Za-z]{43}=$/u;

export const goChecksums: FileRule = (path, _before, after) => {
  const review = emptyReview();
  for (const [index, line] of after.split('\n').entries()) {
    if (line !== '' && !goChecksumLine.test(line)) {
      review.findings.push(`${path}:${index + 1}: not a module checksum: ${quote(line)}`);
    }
  }
  return review;
};

const miseSection = /^\[\[tools\.(?<tool>[\w-]+)\]\]$/u;
const miseEntry = /^(?<key>backend|checksum|url|url_api|version) = "(?<value>[^"\\]*)"$/u;
const miseSpecifiers = /^specifiers = \["(?<value>[^"\\]*)"\]$/u;
const miseChecksum = /^sha256:[a-f\d]{64}$/u;
// mise records where a checksum came from; the checksum itself is what the review compares.
const miseChecksumSource = /^checksum_source = "[a-z][a-z-]*"$/u;

const coreDownloads = new Map([
  ['core:go', 'https://dl.google.com/go/'],
  ['core:node', 'https://nodejs.org/dist/'],
  ['core:bun', 'https://github.com/oven-sh/bun/releases/download/'],
]);

function officialDownloadPrefix(key: string, backend: string): string | undefined {
  if (backend.startsWith('aqua:')) {
    const repository = backend.slice('aqua:'.length);
    return key === 'url_api'
      ? `https://api.github.com/repos/${repository}/releases/assets/`
      : `https://github.com/${repository}/releases/download/`;
  }
  return key === 'url' ? coreDownloads.get(backend) : undefined;
}

function downloadProblem(key: string, value: string, backend: string): string | undefined {
  const url = URL.parse(value);
  const prefix = officialDownloadPrefix(key, backend);
  const plain = url?.protocol === 'https:' && !url.port && !url.username && !url.password && !url.search && !url.hash;
  const official = plain && prefix !== undefined && url.href.startsWith(prefix);
  return official ? undefined : `${value} is not an official download of ${backend || 'an unknown backend'}`;
}

const misePlatformSection = /^\[tools\.(?<tool>[\w-]+)\."platforms\.(?<platform>[\w-]+)"\]$/u;
const misePlatformVariant = /-(?:musl(?:-baseline)?|baseline)$/u;

function miseSections(text: string): string[][] {
  const sections: string[][] = [[]];
  for (const line of text.split('\n')) {
    if (line.startsWith('[')) {
      sections.push([line]);
    } else {
      sections.at(-1)?.push(line);
    }
  }
  return sections;
}

function misePlatform(header: string): { readonly base: string; readonly key: string } | undefined {
  const { platform, tool } = misePlatformSection.exec(header)?.groups ?? {};
  if (platform === undefined || tool === undefined) {
    return undefined;
  }
  return { base: `${tool}/${platform.replace(misePlatformVariant, '')}`, key: `${tool}/${platform}` };
}

function misePlatformAsset(section: readonly string[]): string {
  return section
    .slice(1)
    .filter((line) => line !== '' && !miseChecksumSource.test(line))
    .join('\n');
}

// mise adds musl and baseline variants that reuse the base platform's asset; such a copy brings nothing new to review.
function withoutRepeatedPlatformVariants(text: string): string {
  const sections = miseSections(text);
  const assets = new Map<string, string>();
  for (const section of sections) {
    const platform = misePlatform(section[0] ?? '');
    if (platform !== undefined) {
      assets.set(platform.key, misePlatformAsset(section));
    }
  }
  return sections
    .filter((section) => {
      const platform = misePlatform(section[0] ?? '');
      return (
        platform === undefined ||
        platform.base === platform.key ||
        assets.get(platform.base) !== misePlatformAsset(section)
      );
    })
    .flat()
    .join('\n');
}

// oxlint-disable-next-line typescript/prefer-readonly-parameter-types -- Findings are the caller-owned output accumulator.
function miseLockSnapshot(text: string, findings: string[]): Snapshot {
  const values: DependencyValue[] = [];
  let tool = '';
  let backend = '';
  let version = '';
  const lines = withoutRepeatedPlatformVariants(text)
    .split('\n')
    .filter((line) => !miseChecksumSource.test(line))
    .map((line) => {
      const section = miseSection.exec(line)?.groups?.['tool'];
      if (section !== undefined) {
        tool = section;
        backend = '';
        version = '';
        return line;
      }
      const specifier = miseSpecifiers.exec(line)?.groups?.['value'];
      if (specifier !== undefined) {
        if (specifier !== version) {
          findings.push(`${tool} specifiers ${specifier} differ from version ${version}`);
        }
        return `specifiers = ${placeholder('version')}`;
      }
      const { key = '', value = '' } = miseEntry.exec(line)?.groups ?? {};
      switch (key) {
        case 'backend': {
          backend = value;
          return line;
        }
        case 'checksum': {
          if (!miseChecksum.test(value)) {
            return line;
          }
          values.push({ kind: 'opaque', name: `${tool} checksum`, value });
          return `checksum = ${placeholder('opaque')}`;
        }
        case 'url':
        case 'url_api': {
          const problem = downloadProblem(key, value, backend);
          if (problem) {
            findings.push(`${tool}: ${problem}`);
          }
          values.push({ kind: 'opaque', name: `${tool} ${key}`, value });
          return `${key} = ${placeholder('opaque')}`;
        }
        case 'version': {
          version = value;
          values.push({ kind: 'version', name: tool, value });
          return `version = ${placeholder('version')}`;
        }
        default: {
          return line;
        }
      }
    });
  return { template: lines.join('\n'), values };
}

export const miseLock: FileRule = (path, before, after) => {
  const findings: string[] = [];
  const next = miseLockSnapshot(after, findings);
  const review = compareSnapshots(path, miseLockSnapshot(before, []), next);
  review.findings.push(...findings.map((finding) => `${path}: ${finding}`));
  return review;
};

const providerHashes =
  /^ +"(?:h1:[\d+/A-Za-z]{43}=|zh:[a-f\d]{64})",\n(?: +"(?:h1:[\d+/A-Za-z]{43}=|zh:[a-f\d]{64})",\n)*/gmu;

const providerVersions = [
  {
    name: 'provider',
    pattern:
      /^provider "(?<provider>[^"]+)" \{\n +version += "(?<version>\d+\.\d+\.\d+)"\n +constraints += "(?:= )?(?<constraint>\d+\.\d+\.\d+)"$/dgmu,
    values: { constraint: 'version', version: 'version' },
  },
] as const;

function openTofuLockSnapshot(text: string): Snapshot {
  const hashes = `    ${placeholder('opaque')}\n`;
  return snapshot(
    text.replaceAll(providerHashes, () => hashes),
    providerVersions,
  );
}

export const openTofuLock: FileRule = (path, before, after): FileReview =>
  compareSnapshots(path, openTofuLockSnapshot(before), openTofuLockSnapshot(after));
