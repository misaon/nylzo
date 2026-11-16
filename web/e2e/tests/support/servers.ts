import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import path from 'node:path';

const port = Number(process.env['NYLZO_E2E_PORT'] ?? '8090');

export const BASE_URL = `http://127.0.0.1:${port}`;
export const POW_BASE_URL = `http://127.0.0.1:${port + 1}`;
export const OFFICIAL_BASE_URL = `http://127.0.0.1:${port + 2}`;
export const ADMIN_BASE_URL = `http://localhost:${port + 3}`;
export const NYLZO_BINARY = path.join(import.meta.dirname, `../../.bin/${port}/nylzo`);

// Runs a command of the binary that serves baseUrl with the settings serve.ts gave that server.
export function serverCommand(baseUrl: string, parameters: readonly string[]): string {
  const directory = path.join(import.meta.dirname, `../../.bin/${new URL(baseUrl).port}`);
  const settings: unknown = JSON.parse(readFileSync(path.join(directory, 'settings.json'), 'utf8'));
  if (typeof settings !== 'object' || settings === null) {
    throw new TypeError(`serve.ts wrote no settings for ${baseUrl}`);
  }
  return execFileSync(path.join(directory, 'nylzo'), parameters, {
    encoding: 'utf8',
    env: { ...process.env, ...settings },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
}
