import { existsSync, readFileSync, writeFileSync } from 'node:fs';

interface AssetPin {
  readonly sha256: string;
  readonly version: string;
}

interface ToolchainPin {
  readonly amd64: string;
  readonly arm64: string;
  readonly version: string;
}

const hostTools = ['gh', 'cosign', 'jq'] as const;

function hostToolPin(assets: Readonly<Record<string, AssetPin>>, tool: string): ToolchainPin {
  const amd64 = assets[`${tool}-amd64`];
  const arm64 = assets[`${tool}-arm64`];
  if (amd64 === undefined || arm64 === undefined) {
    throw new Error(`Pin both architectures of ${tool}`);
  }
  if (amd64.version !== arm64.version) {
    throw new Error(`Both architectures of ${tool} must pin the same release`);
  }
  return { version: amd64.version.replace(/^(?:v|jq-)/u, ''), amd64: amd64.sha256, arm64: arm64.sha256 };
}

function captured(source: string, pattern: RegExp, description: string): string {
  const value = pattern.exec(source)?.[1];
  if (value === undefined) {
    throw new Error(`Cannot find ${description}`);
  }
  return value;
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isAssetMap(value: unknown): value is Record<string, AssetPin> {
  return (
    isObject(value) &&
    Object.values(value).every(
      (pin: unknown) =>
        isObject(pin) &&
        typeof pin['version'] === 'string' &&
        typeof pin['sha256'] === 'string' &&
        /^[a-f\d]{64}$/u.test(pin['sha256']),
    )
  );
}

const assets: unknown = JSON.parse(readFileSync('tools/dependencies/assets.json', 'utf8'));
const toolchain: unknown = JSON.parse(readFileSync('deploy/hetzner-flatcar/toolchain.json', 'utf8'));
if (!isAssetMap(assets) || !isObject(toolchain)) {
  throw new TypeError('Invalid host toolchain or binary asset manifest');
}
for (const tool of hostTools) {
  toolchain[tool] = hostToolPin(assets, tool);
}
let deployTask: string | undefined;
if ('opentofu' in toolchain) {
  const opentofu = captured(
    readFileSync('mise.toml', 'utf8'),
    /^opentofu = "([^"]+)"$/mu,
    'the OpenTofu pin in mise.toml',
  );
  toolchain['opentofu'] = opentofu;
  deployTask = readFileSync('mise-tasks/deploy', 'utf8').replace(
    /opentofu@\d+\.\d+\.\d+/u,
    () => `opentofu@${opentofu}`,
  );
}
toolchain['caddy'] = existsSync('deploy/caddy/go.mod')
  ? captured(
      readFileSync('deploy/caddy/go.mod', 'utf8'),
      /^(?:require )?\s*github\.com\/caddyserver\/caddy\/v2 v(\d+\.\d+\.\d+)$/mu,
      'the Caddy module in deploy/caddy/go.mod',
    )
  : captured(
      readFileSync('deploy/compose/compose.yaml', 'utf8'),
      /image: caddy:(\d+\.\d+\.\d+)@sha256:/u,
      'the Caddy image in deploy/compose/compose.yaml',
    );
writeFileSync('deploy/hetzner-flatcar/toolchain.json', `${JSON.stringify(toolchain, undefined, 2)}\n`);
if (deployTask !== undefined) {
  writeFileSync('mise-tasks/deploy', deployTask);
}

console.log('Synchronized the host toolchain pins with the reviewed dependency pins.');
