import { execFileSync } from 'node:child_process';

export type GoImports = Readonly<Record<string, readonly string[]>>;

export const releasePlatforms = [
  'linux/amd64',
  'linux/arm64',
  'darwin/amd64',
  'darwin/arm64',
  'windows/amd64',
  'windows/arm64',
] as const;

export function collectGoImports(repositoryRoot: string): Record<string, string[]> {
  return Object.fromEntries(releasePlatforms.map((platform) => [platform, importsFor(platform, repositoryRoot)]));
}

function importsFor(platform: string, repositoryRoot: string): string[] {
  const [operatingSystem, architecture] = platform.split('/', 2);
  const output = execFileSync('go', ['list', '-mod=readonly', '-deps', '-f', '{{.ImportPath}}', './cmd/nylzo'], {
    cwd: repositoryRoot,
    encoding: 'utf8',
    env: { ...process.env, CGO_ENABLED: '0', GOARCH: architecture, GOOS: operatingSystem, GOTOOLCHAIN: 'local' },
    maxBuffer: 4 * 1024 * 1024,
  });
  return output.trim().split('\n');
}
