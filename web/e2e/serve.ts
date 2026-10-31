import { execFileSync, spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdirSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const [port = '8090', profile = 'default'] = process.argv.slice(2);
const repository = path.resolve(import.meta.dirname, '../..');
const binaryDirectory = path.join(import.meta.dirname, '.bin', port);
const binary = path.join(binaryDirectory, 'nylzo');

const profiles: Record<string, Record<string, string>> = {
  default: {},
  pow: { NYLZO_GLOBAL_DAILY_CREATES: '8', NYLZO_POW_DIFFICULTY: '4' },
};

mkdirSync(binaryDirectory, { recursive: true });
execFileSync(process.execPath, [path.join(repository, 'tools/build.mts'), binary], {
  cwd: repository,
  stdio: 'inherit',
});

const dataDirectory = mkdtempSync(path.join(tmpdir(), 'nylzo-e2e-'));
const keyDirectory = mkdtempSync(path.join(tmpdir(), 'nylzo-e2e-key-'));
const reportKey = execFileSync(binary, ['report-key', '--key-file', path.join(keyDirectory, 'report-key')], {
  encoding: 'utf8',
})
  .trim()
  .replace(/^NYLZO_REPORT_KEY=/, '');

const server = spawn(binary, ['serve'], {
  stdio: 'inherit',
  env: {
    ...process.env,
    NYLZO_LISTEN: `127.0.0.1:${port}`,
    NYLZO_PUBLIC_URL: `http://127.0.0.1:${port}`,
    NYLZO_DATA_DIR: dataDirectory,
    NYLZO_PEPPER: randomBytes(32).toString('hex'),
    NYLZO_REPORT_KEY: reportKey,
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
server.on('exit', (code) => {
  process.exitCode = code ?? 1;
});
