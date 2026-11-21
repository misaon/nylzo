import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import path from 'node:path';

const DEFAULT_PORT = 8090;
const HIGHEST_FIRST_PORT = 65_532;

function parsePort(value: string | undefined): number {
  if (value === undefined) {
    return DEFAULT_PORT;
  }
  const first = Number(value);
  if (!/^\d+$/u.test(value) || first < 1 || first > HIGHEST_FIRST_PORT) {
    throw new RangeError(
      `NYLZO_E2E_PORT must be a port from 1 to ${HIGHEST_FIRST_PORT}, the first of four in a row, not "${value}"`,
    );
  }
  return first;
}

const port = parsePort(process.env['NYLZO_E2E_PORT']);

export const BASE_URL = `http://127.0.0.1:${port}`;
export const POW_BASE_URL = `http://127.0.0.1:${port + 1}`;
export const OFFICIAL_BASE_URL = `http://127.0.0.1:${port + 2}`;
export const ADMIN_BASE_URL = `http://localhost:${port + 3}`;
export const NYLZO_BINARY = path.join(import.meta.dirname, `../../.bin/${port}/nylzo`);

function isSettings(value: unknown): value is Readonly<Record<string, string>> {
  return (
    typeof value === 'object' &&
    value !== null &&
    !Array.isArray(value) &&
    Object.values(value).every((setting) => typeof setting === 'string')
  );
}

// Runs a command of the binary that serves baseUrl with the settings serve.ts gave that server.
export function serverCommand(baseUrl: string, parameters: readonly string[]): string {
  const directory = path.join(import.meta.dirname, `../../.bin/${new URL(baseUrl).port}`);
  const settings: unknown = JSON.parse(readFileSync(path.join(directory, 'settings.json'), 'utf8'));
  if (!isSettings(settings)) {
    throw new TypeError(`serve.ts wrote no settings for ${baseUrl}`);
  }
  return execFileSync(path.join(directory, 'nylzo'), parameters, {
    encoding: 'utf8',
    env: { ...process.env, ...settings },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
}
