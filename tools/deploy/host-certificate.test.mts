import assert from "node:assert/strict";
import { execFile, execFileSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { createServer } from "node:tls";
import { promisify } from "node:util";
import test from "node:test";

const execute = promisify(execFile);

test("host certificate checks verify the live TLS identity, trust, and renewal reserve without reading Caddy storage", { timeout: 60000 }, async () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-host-certificate-"));
  try {
    const bin = join(directory, "bin"), output = join(directory, "status");
    mkdirSync(bin); mkdirSync(output);
    for (const [name, body] of Object.entries({
      "host-policy": "exit 1",
      systemctl: 'if [ "$1" = is-active ]; then exit 3; fi; printf "success\\n"',
      df: 'printf "Filesystem Available\\nfixture 1 1 16777216\\n"',
      update_engine_client: "exit 1",
    })) {
      writeFileSync(join(bin, name), `#!/bin/sh\n${body}\n`);
      chmodSync(join(bin, name), 0o700);
    }
    writeFileSync(join(directory, "caddy.env"), "NYLZO_DOMAIN=nylzo.example.org\n");
    for (const [name, domain, days] of [["valid", "nylzo.example.org", "7"], ["foreign", "other.example.org", "7"], ["renewal-due", "nylzo.example.org", "1"]]) {
      execFileSync("openssl", ["req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes", "-days", days!, "-subj", `/CN=${domain}`, "-addext", `subjectAltName=DNS:${domain}`, "-addext", "extendedKeyUsage=serverAuth", "-keyout", join(directory, `${name}.key`), "-out", join(directory, `${name}.crt`)], { stdio: "pipe", timeout: 10000 });
    }
    const run = async (endpoint: string, ca: string) => {
      await execute("bash", [resolve("deploy/hetzner-flatcar/host/monitoring/host-status")], { timeout: 15000, env: {
        ...process.env, PATH: `${bin}:${process.env.PATH}`, SSL_CERT_FILE: join(directory, `${ca}.crt`), SSL_CERT_DIR: directory,
        NYLZO_HOST_STATUS_DIR: output, NYLZO_DEPLOY_STATE: directory, NYLZO_CLOCK_STATE: join(directory, "no-clock"),
        NYLZO_CADDY_ENV: join(directory, "caddy.env"), NYLZO_CERTIFICATE_ENDPOINT: endpoint,
      } });
      return JSON.parse(readFileSync(join(output, "status.json"), "utf8")).checks.certificate;
    };
    for (const [identity, ca, expected] of [["valid", "valid", true], ["valid", "foreign", false], ["foreign", "foreign", false], ["renewal-due", "renewal-due", false]] as const) {
      const server = createServer({ key: readFileSync(join(directory, `${identity}.key`)), cert: readFileSync(join(directory, `${identity}.crt`)), minVersion: "TLSv1.3" }, socket => socket.end());
      server.on("tlsClientError", () => {});
      await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve));
      try {
        const address = server.address();
        assert(address && typeof address !== "string");
        assert.equal(await run(`127.0.0.1:${address.port}`, ca), expected, `${identity} certificate with ${ca} trust anchor`);
      } finally { await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve())); }
    }
    assert.equal(await run("127.0.0.1:0", "valid"), false, "An unavailable TLS listener must fail closed");
    assert.equal(await run("192.0.2.1:443", "valid"), false, "The local certificate probe must reject non-loopback endpoints");
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
