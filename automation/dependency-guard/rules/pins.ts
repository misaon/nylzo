import {
  compareSnapshots,
  type DependencyValue,
  type Extraction,
  type FileRule,
  placeholder,
  type Snapshot,
  snapshot,
} from './file-review.ts';

function pinRule(extractions: readonly Extraction[]): FileRule {
  return (path, before, after) => compareSnapshots(path, snapshot(before, extractions), snapshot(after, extractions));
}

export const miseToolVersions = pinRule([
  { name: 'name', pattern: /^(?<name>[\w-]+) = "(?<version>v?\d+\.\d+\.\d+)"$/dgm, values: { version: 'version' } },
  { name: 'govulncheck', pattern: /govulncheck@(?<version>v\d+\.\d+\.\d+) /dg, values: { version: 'version' } },
]);

export const nodeVersion = pinRule([
  { name: 'node', pattern: /^(?<version>\d+\.\d+\.\d+)$/dgm, values: { version: 'version' } },
]);

export const deployTask = pinRule([
  { name: 'opentofu', pattern: /mise exec opentofu@(?<version>\d+\.\d+\.\d+) /dg, values: { version: 'version' } },
]);

export const openTofuVersions = pinRule([
  {
    name: 'setting',
    pattern: /^ +(?<setting>required_version|version) += "= (?<version>\d+\.\d+\.\d+)"$/dgm,
    values: { version: 'version' },
  },
]);

export const releaseBaseImage = pinRule([
  {
    name: 'distroless',
    pattern: /base_image: gcr\.io\/distroless\/static-debian13:nonroot@sha256:(?<digest>[a-f\d]{64})$/dgm,
    values: { digest: 'opaque' },
  },
]);

export const runtimeTest = pinRule([
  {
    name: 'ubuntu',
    pattern: /"ubuntu:(?<tag>\d+\.\d+)@sha256:(?<digest>[a-f\d]{64})"/dg,
    values: { digest: 'opaque', tag: 'fixed' },
  },
]);

const goDirectives = [
  { block: '', name: 'go', pattern: /^go (?<version>\d+\.\d+(?:\.\d+)?)$/d },
  { block: '', name: 'toolchain', pattern: /^toolchain go(?<version>\d+\.\d+\.\d+)$/d },
  {
    block: '',
    name: 'module',
    pattern: /^require (?<module>[^\s()]+) (?<version>v\d+\.\d+\.\d+(?:[-+][\w+.-]*)?)(?: \/\/ indirect)?$/d,
  },
  {
    block: 'require',
    name: 'module',
    pattern: /^\t(?<module>[^\s()]+) (?<version>v\d+\.\d+\.\d+(?:[-+][\w+.-]*)?)(?: \/\/ indirect)?$/d,
  },
];

function goModuleSnapshot(text: string): Snapshot {
  const values: DependencyValue[] = [];
  let block = '';
  const lines = text.split('\n').map((line) => {
    const opening = /^(?<keyword>[a-z]+) \($/.exec(line);
    if (opening) {
      block = opening.groups?.['keyword'] ?? '';
      return line;
    }
    if (line === ')') {
      block = '';
      return line;
    }
    for (const directive of goDirectives) {
      const match = directive.block === block ? directive.pattern.exec(line) : null;
      const span = match?.indices?.groups?.['version'];
      if (match && span) {
        values.push({ kind: 'version', name: match.groups?.['module'] ?? directive.name, value: line.slice(...span) });
        return line.slice(0, span[0]) + placeholder('version') + line.slice(span[1]);
      }
    }
    return line;
  });
  return { template: lines.join('\n'), values };
}

export const goModule: FileRule = (path, before, after) =>
  compareSnapshots(path, goModuleSnapshot(before), goModuleSnapshot(after));
