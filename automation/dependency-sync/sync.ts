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

function hostToolPin(assets: Record<string, AssetPin>, tool: string): ToolchainPin {
  const amd64 = assets[`${tool}-amd64`];
  const arm64 = assets[`${tool}-arm64`];
  if (amd64 === undefined || arm64 === undefined) {
    throw new Error(`Pin both architectures of ${tool}`);
  }
  if (amd64.version !== arm64.version) {
    throw new Error(`Both architectures of ${tool} must pin the same release`);
  }
  return { version: amd64.version.replace(/^(?:v|jq-)/, ''), amd64: amd64.sha256, arm64: arm64.sha256 };
}

function captured(source: string, pattern: RegExp, description: string): string {
  const value = pattern.exec(source)?.[1];
  if (value === undefined) {
    throw new Error(`Cannot find ${description}`);
  }
  return value;
}

const assets = JSON.parse(readFileSync('tools/dependencies/assets.json', 'utf8')) as Record<string, AssetPin>;
const toolchain = JSON.parse(readFileSync('deploy/hetzner-flatcar/toolchain.json', 'utf8')) as Record<string, unknown>;
for (const tool of hostTools) {
  toolchain[tool] = hostToolPin(assets, tool);
}
const opentofu = captured(
  readFileSync('mise.toml', 'utf8'),
  /^opentofu = "([^"]+)"$/m,
  'the OpenTofu pin in mise.toml',
);
toolchain['opentofu'] = opentofu;
toolchain['caddy'] = existsSync('deploy/caddy/go.mod')
  ? captured(
      readFileSync('deploy/caddy/go.mod', 'utf8'),
      /^(?:require )?\s*github\.com\/caddyserver\/caddy\/v2 v(\d+\.\d+\.\d+)$/m,
      'the Caddy module in deploy/caddy/go.mod',
    )
  : captured(
      readFileSync('deploy/compose/compose.yaml', 'utf8'),
      /image: caddy:(\d+\.\d+\.\d+)@sha256:/,
      'the Caddy image in deploy/compose/compose.yaml',
    );
writeFileSync('deploy/hetzner-flatcar/toolchain.json', `${JSON.stringify(toolchain, undefined, 2)}\n`);

const deployTask = readFileSync('mise-tasks/deploy', 'utf8');
writeFileSync(
  'mise-tasks/deploy',
  deployTask.replace(/opentofu@\d+\.\d+\.\d+/, () => `opentofu@${opentofu}`),
);
console.log('Synchronized the host toolchain pins with the reviewed dependency pins.');
