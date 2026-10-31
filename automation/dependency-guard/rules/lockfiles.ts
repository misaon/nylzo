import {
  compareSnapshots,
  type DependencyValue,
  emptyReview,
  type FileReview,
  type FileRule,
  placeholder,
  quote,
  type Snapshot,
  snapshot,
} from './file-review.ts';

const deprecation = /^ +deprecated: /;
const integrityResolution = /^ +resolution: \{integrity: sha(?:256|384|512)-[\d+/A-Za-z]+=*\}$/;
const escapedOrAliased = /\\|[&*][\w-]/;
const customSourceKey = /(?:^|[\s,{])["']?(?:commit|repo|tarball)["']?\s*:/;
const externalSource = /\b(?:[a-z][\d+.a-z-]*:\/\/|git\+|git@|file:|link:|github:|gitlab:|bitbucket:)/i;
const workspaceLink = /\blink:(?<target>[^\s"',}]*)/g;

function documentHeaders(lockfile: string): string[] {
  return lockfile.split(/^---$/m).map((document) => document.split(/^(?:importers|packages):$/m, 1)[0] ?? '');
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
  if (line.includes('resolution:') && !integrityResolution.test(line)) {
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

const goChecksumLine = /^[\w.~+!/-]+ v\S+ h1:[\d+/A-Za-z]{43}=$/;

export const goChecksums: FileRule = (path, _before, after) => {
  const review = emptyReview();
  for (const [index, line] of after.split('\n').entries()) {
    if (line !== '' && !goChecksumLine.test(line)) {
      review.findings.push(`${path}:${index + 1}: not a module checksum: ${quote(line)}`);
    }
  }
  return review;
};

const miseSection = /^\[\[tools\.(?<tool>[\w-]+)\]\]$/;
const miseEntry = /^(?<key>backend|checksum|url|url_api|version) = "(?<value>[^"\\]*)"$/;
const miseSpecifiers = /^specifiers = \["(?<value>[^"\\]*)"\]$/;
const miseChecksum = /^sha256:[a-f\d]{64}$/;

const coreDownloads = new Map([
  ['core:go', 'https://dl.google.com/go/'],
  ['core:node', 'https://nodejs.org/dist/'],
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

function miseLockSnapshot(text: string, findings: string[]): Snapshot {
  const values: DependencyValue[] = [];
  let tool = '';
  let backend = '';
  let version = '';
  const lines = text.split('\n').map((line) => {
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
  /^ +"(?:h1:[\d+/A-Za-z]{43}=|zh:[a-f\d]{64})",\n(?: +"(?:h1:[\d+/A-Za-z]{43}=|zh:[a-f\d]{64})",\n)*/gm;

const providerVersions = [
  {
    name: 'provider',
    pattern:
      /^provider "(?<provider>[^"]+)" \{\n +version += "(?<version>\d+\.\d+\.\d+)"\n +constraints += "(?:= )?(?<constraint>\d+\.\d+\.\d+)"$/dgm,
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
