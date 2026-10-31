import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import path from 'node:path';

export interface GoBuild {
  readonly command: string;
  readonly directory: string;
  readonly platforms: readonly string[];
  readonly reviewsUnlinkedModules: boolean;
  readonly tagsFile?: string;
}

export interface LinkedCode {
  readonly modules: readonly string[];
  readonly packages: readonly string[];
}

export type GoImports = Readonly<Record<string, Readonly<Record<string, LinkedCode>>>>;

export const releasePlatforms = [
  'linux/amd64',
  'linux/arm64',
  'darwin/amd64',
  'darwin/arm64',
  'windows/amd64',
  'windows/arm64',
] as const;

export const goBuilds: Readonly<Record<string, GoBuild>> = {
  'go.mod': {
    command: 'github.com/misaon/nylzo/cmd/nylzo',
    directory: '.',
    platforms: releasePlatforms,
    reviewsUnlinkedModules: true,
  },
  'deploy/caddy/go.mod': {
    command: 'github.com/misaon/nylzo/deploy/caddy',
    directory: 'deploy/caddy',
    platforms: ['linux/amd64', 'linux/arm64'],
    reviewsUnlinkedModules: false,
    tagsFile: 'deploy/caddy/build-tags',
  },
};

export function collectGoImports(repositoryRoot: string): GoImports {
  return Object.fromEntries(
    Object.entries(goBuilds).map(([lockfile, build]) => [
      lockfile,
      Object.fromEntries(build.platforms.map((platform) => [platform, linkedCode(build, platform, repositoryRoot)])),
    ]),
  );
}

function buildTags(build: GoBuild, repositoryRoot: string): string {
  if (build.tagsFile === undefined) {
    return '';
  }
  const tags = readFileSync(path.join(repositoryRoot, build.tagsFile), 'utf8').trim();
  if (!/^\w+(?:,\w+)*$/.test(tags)) {
    throw new Error(`${build.tagsFile} must list comma-separated build tags`);
  }
  return tags;
}

function linkedCode(build: GoBuild, platform: string, repositoryRoot: string): LinkedCode {
  const [operatingSystem, architecture] = platform.split('/', 2);
  const output = execFileSync(
    'go',
    [
      'list',
      '-mod=readonly',
      '-deps',
      `-tags=${buildTags(build, repositoryRoot)}`,
      '-f',
      '{{.ImportPath}}\t{{with .Module}}{{.Path}}{{end}}',
      build.command,
    ],
    {
      cwd: path.join(repositoryRoot, build.directory),
      encoding: 'utf8',
      env: { ...process.env, CGO_ENABLED: '0', GOARCH: architecture, GOOS: operatingSystem, GOTOOLCHAIN: 'local' },
      maxBuffer: 4 * 1024 * 1024,
    },
  );
  const lines = output
    .trim()
    .split('\n')
    .map((line) => line.split('\t'));
  const modules = new Set(lines.map(([, module = '']) => module).filter((module) => module !== ''));
  return {
    modules: [...modules].toSorted((left, right) => left.localeCompare(right)),
    packages: lines.map(([importPath = '']) => importPath),
  };
}
