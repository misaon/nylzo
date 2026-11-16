import { execFileSync, spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdirSync, mkdtempSync, openSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const [port = '8090', profile = 'default'] = process.argv.slice(2);
const repository = path.resolve(import.meta.dirname, '../..');
const binaryDirectory = path.join(import.meta.dirname, '.bin', port);
const binary = path.join(binaryDirectory, 'nylzo');

const profiles: Record<string, Record<string, string>> = {
  default: {},
  pow: { NYLZO_GLOBAL_DAILY_CREATES: '8', NYLZO_POW_DIFFICULTY: '4' },
  official: { NYLZO_PUBLIC_URL: 'https://nylzo.com' },
  // WebAuthn refuses an IP address as the relying party, so the admin area runs on localhost without a public URL.
  admin: { NYLZO_PUBLIC_URL: '' },
};

mkdirSync(binaryDirectory, { recursive: true });
execFileSync(process.execPath, [path.join(repository, 'tools/build.mts'), binary], {
  cwd: repository,
  stdio: 'inherit',
});

const dataDirectory = mkdtempSync(path.join(tmpdir(), 'nylzo-e2e-'));
const keyDirectory = mkdtempSync(path.join(tmpdir(), 'nylzo-e2e-key-'));
const pepperFile = path.join(keyDirectory, 'pepper');
writeFileSync(pepperFile, randomBytes(32).toString('hex'), { mode: 0o400 });

// The server's notices about the test host stay out of the CI log; a failed run uploads them with its results.
const logDirectory = path.join(import.meta.dirname, 'server-logs');
mkdirSync(logDirectory, { recursive: true });
const log = openSync(path.join(logDirectory, `server-${port}.log`), 'w');
const settings = {
  NYLZO_LISTEN: `127.0.0.1:${port}`,
  NYLZO_PUBLIC_URL: `http://127.0.0.1:${port}`,
  NYLZO_DATA_DIR: dataDirectory,
  NYLZO_PEPPER_FILE: pepperFile,
  NYLZO_ALLOW_SWAP: '1',
  NYLZO_ALLOW_UNERASABLE_STORAGE: process.platform === 'linux' ? '0' : '1',
  NYLZO_ALLOW_UNERASED_MEMORY: process.platform === 'linux' ? '0' : '1',
  NYLZO_RATE_CREATE_HOUR: '0',
  NYLZO_RATE_CREATE_DAY: '0',
  NYLZO_RATE_CREATE_BYTES_DAY: '0',
  NYLZO_RATE_READ_HOUR: '0',
  NYLZO_RATE_SOURCE_RPS: '1000',
  NYLZO_RATE_SOURCE_BURST: '1000',
  NYLZO_RATE_BUCKET_RPS: '2000',
  NYLZO_RATE_BUCKET_BURST: '2000',
  NYLZO_OPERATOR: 'Nylzo e2e',
  NYLZO_CONTACT: 'abuse@nylzo.test',
  NYLZO_HOSTING: 'localhost',
  ...profiles[profile],
};
// A test runs the server's own commands, such as admin-enroll, with the settings of the server it talks to.
writeFileSync(path.join(binaryDirectory, 'settings.json'), JSON.stringify(settings));
const server = spawn(binary, ['serve'], { stdio: ['ignore', log, log], env: { ...process.env, ...settings } });

for (const signal of ['SIGINT', 'SIGTERM'] as const) {
  process.on(signal, () => {
    server.kill(signal);
  });
}
server.on('exit', (code) => {
  process.exitCode = code ?? 1;
});
