import path from 'node:path';

const port = Number(process.env['NYLZO_E2E_PORT'] ?? '8090');

export const BASE_URL = `http://127.0.0.1:${port}`;
export const POW_BASE_URL = `http://127.0.0.1:${port + 1}`;
export const NYLZO_BINARY = path.join(import.meta.dirname, `../../.bin/${port}/nylzo`);
