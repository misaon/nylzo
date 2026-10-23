import { execFileSync, spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdirSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const [port = '8090', profile = 'default'] = process.argv.slice(2);
const root = new URL('../../', import.meta.url).pathname;
const dir = mkdtempSync(join(tmpdir(), 'nylzo-e2e-'));
const binary = new URL(`./.bin/${port}/nylzo`, import.meta.url).pathname;
mkdirSync(new URL(`./.bin/${port}/`, import.meta.url), { recursive: true });

const profiles: Record<string, Record<string, string>> = {
  default: {},
  pow: { NYLZO_GLOBAL_DAILY_CREATES: '8', NYLZO_POW_DIFFICULTY: '4' },
};

execFileSync(process.env['NYLZO_GO'] ?? 'go', ['build', '-o', binary, './cmd/nylzo'], { cwd: root, stdio: 'inherit' });

const server = spawn(binary, ['serve'], {
  stdio: 'inherit',
  env: {
    ...process.env,
    NYLZO_LISTEN: `127.0.0.1:${port}`,
    NYLZO_DATA_DIR: dir,
    NYLZO_PEPPER: randomBytes(32).toString('hex'),
    NYLZO_RATE_CREATE_HOUR: '0',
    NYLZO_RATE_CREATE_DAY: '0',
    NYLZO_RATE_CREATE_BYTES_DAY: '0',
    NYLZO_RATE_READ_HOUR: '0',
    NYLZO_RATE_ABUSE_HOUR: '0',
    NYLZO_RATE_SOURCE_RPS: '1000',
    NYLZO_RATE_SOURCE_BURST: '1000',
    NYLZO_RATE_BUCKET_RPS: '2000',
    NYLZO_RATE_BUCKET_BURST: '2000',
    NYLZO_OPERATOR: 'Nylzo e2e',
    NYLZO_CONTACT: 'abuse@nylzo.test',
    NYLZO_HOSTING: 'localhost',
    ...profiles[profile],
  },
});

for (const signal of ['SIGINT', 'SIGTERM'] as const) {
  process.on(signal, () => server.kill(signal));
}
server.on('exit', (code) => process.exit(code ?? 1));
