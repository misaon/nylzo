import { compareSnapshots, placeholder, snapshot } from './file-review.ts';
import type { DependencyValue, Extraction, FileRule, Snapshot } from './file-review.ts';

function pinRule(extractions: readonly Extraction[]): FileRule {
  return (path, before, after) => compareSnapshots(path, snapshot(before, extractions), snapshot(after, extractions));
}

export const miseToolVersions = pinRule([
  {
    name: 'name',
    pattern: /^"?(?<name>(?:[a-z]+:)?[\w./-]+)"? = "(?<version>v?\d+\.\d+\.\d+)"$/dgmu,
    values: { version: 'version' },
  },
  { name: 'govulncheck', pattern: /govulncheck@(?<version>v\d+\.\d+\.\d+) /dgu, values: { version: 'version' } },
]);

export const nodeVersion = pinRule([
  { name: 'node', pattern: /^(?<version>\d+\.\d+\.\d+)$/dgmu, values: { version: 'version' } },
]);

export const deployTask = pinRule([
  { name: 'opentofu', pattern: /mise exec opentofu@(?<version>\d+\.\d+\.\d+) /dgu, values: { version: 'version' } },
]);

export const openTofuVersions = pinRule([
  {
    name: 'setting',
    pattern: /^ +(?<setting>opentofu|required_version|version) += "= (?<version>\d+\.\d+\.\d+)"$/dgmu,
    values: { version: 'version' },
  },
]);

export const releaseBaseImage = pinRule([
  {
    name: 'distroless',
    pattern: /base_image: gcr\.io\/distroless\/static-debian13:nonroot@sha256:(?<digest>[a-f\d]{64})$/dgmu,
    values: { digest: 'opaque' },
  },
]);

export const composeFile = pinRule([
  {
    name: 'caddy',
    pattern: /^ {4}image: caddy:(?<version>\d+\.\d+\.\d+)@sha256:(?<digest>[a-f\d]{64})$/dgmu,
    values: { digest: 'opaque', version: 'version' },
  },
]);

export const runtimeTest = pinRule([
  {
    name: 'ubuntu',
    pattern: /"ubuntu:(?<tag>\d+\.\d+)@sha256:(?<digest>[a-f\d]{64})"/dgu,
    values: { digest: 'opaque', tag: 'fixed' },
  },
]);

const goDirective = /^go (?<version>\d+\.\d+(?:\.\d+)?)$/du;
const toolchainDirective = /^toolchain go(?<version>\d+\.\d+\.\d+)$/u;
const requirements = [
  {
    block: '',
    pattern: /^require (?<module>[^\s()]+) (?<version>v\d+\.\d+\.\d+(?:[-+][\w+.-]*)?)(?: \/\/ indirect)?$/du,
  },
  {
    block: 'require',
    pattern: /^\t(?<module>[^\s()]+) (?<version>v\d+\.\d+\.\d+(?:[-+][\w+.-]*)?)(?: \/\/ indirect)?$/du,
  },
];

function goRelease(version: string): string {
  return /^\d+\.\d+$/u.test(version) ? `${version}.0` : version;
}

function goLanguage(release: string): string {
  return release.split('.').slice(0, 2).join('.');
}

// go mod tidy drops a toolchain line equal to the go line; Go then runs the release the go line names.
function withoutToolchain(lines: readonly string[]): { lines: readonly string[]; toolchain: string | undefined } {
  const index = lines.findIndex((line) => toolchainDirective.test(line));
  const toolchain = toolchainDirective.exec(lines[index] ?? '')?.groups?.['version'];
  if (toolchain === undefined) {
    return { lines, toolchain };
  }
  const blankOnBothSides = lines[index - 1] === '' && lines[index + 1] === '';
  return { lines: lines.toSpliced(index, blankOnBothSides ? 2 : 1), toolchain };
}

function goModuleSnapshot(text: string): Snapshot {
  const values: DependencyValue[] = [];
  const { lines, toolchain } = withoutToolchain(text.split('\n'));
  let block = '';
  let goVersion: string | undefined;
  const template = lines.map((line) => {
    const opening = /^(?<keyword>[a-z]+) \($/u.exec(line);
    if (opening) {
      block = opening.groups?.['keyword'] ?? '';
      return line;
    }
    if (line === ')') {
      block = '';
      return line;
    }
    const go = block === '' ? goDirective.exec(line) : null;
    const goSpan = go?.indices?.groups?.['version'];
    if (goSpan) {
      goVersion = goRelease(line.slice(...goSpan));
      values.push(
        { kind: 'fixed', name: 'go language', value: goLanguage(goVersion) },
        { kind: 'version', name: 'go', value: goVersion },
      );
      return line.slice(0, goSpan[0]) + placeholder('version') + line.slice(goSpan[1]);
    }
    for (const requirement of requirements) {
      const match = requirement.block === block ? requirement.pattern.exec(line) : null;
      const span = match?.indices?.groups?.['version'];
      const module = match?.groups?.['module'];
      if (span && module !== undefined) {
        values.push({ kind: 'version', name: module, value: line.slice(...span) });
        return line.slice(0, span[0]) + placeholder('version') + line.slice(span[1]);
      }
    }
    return line;
  });
  const effectiveToolchain = toolchain ?? goVersion;
  if (effectiveToolchain !== undefined) {
    values.push({ kind: 'version', name: 'toolchain', value: effectiveToolchain });
  }
  return { template: template.join('\n'), values };
}

export const goModule: FileRule = (path, before, after) =>
  compareSnapshots(path, goModuleSnapshot(before), goModuleSnapshot(after));
