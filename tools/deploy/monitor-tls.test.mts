import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { request } from "node:https";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";
import { readHostStatus, requireClientAuthentication, validateHostStatus } from "../security/monitor-host.mts";

const available = spawnSync("docker", ["info"], { stdio: "ignore" }).status === 0;
const docker = (...args: string[]) => execFileSync("docker", args, { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], timeout: 120000 });

test("Caddy serves host status only to the pinned mTLS identity, isolated from the public listener", { skip: !available, timeout: 180000 }, async () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-monitor-tls-"));
  const fixtureDirectory = join(directory, "public");
  mkdirSync(fixtureDirectory);
  const name = `nylzo-monitor-test-${randomBytes(6).toString("hex")}`;
  try {
    for (const identity of ["trusted", "foreign"]) execFileSync("go", ["run", "./tools/deploy/monitor-identity", join(directory, identity)], { stdio: "pipe" });
    const publicClient = readFileSync(join(directory, "trusted/client.pem"), "utf8");
    const key = readFileSync(join(directory, "trusted/client-key.pem"), "utf8");
    const foreign = { cert: readFileSync(join(directory, "foreign/client.pem"), "utf8"), key: readFileSync(join(directory, "foreign/client-key.pem"), "utf8") };
    writeFileSync(join(fixtureDirectory, "client.pem"), publicClient);
    const healthy = { version: 1, observedAt: Math.floor(Date.now() / 1000), checks: { hostPolicy: true, services: true, diskReserve: true, applicationUpdates: true, osUpdates: true } };
    writeFileSync(join(fixtureDirectory, "status.json"), JSON.stringify(healthy));
    let configuration = readFileSync(resolve("deploy/hetzner-flatcar/runtime/Caddyfile"), "utf8") + "\n" + readFileSync(resolve("deploy/hetzner-flatcar/runtime/monitor.Caddyfile"), "utf8");
    configuration = configuration.replace("    admin off", "    admin off\n    local_certs\n    skip_install_trust\n    https_port 14443\n    http_port 18080");
    configuration = configuration.replace(/    reverse_proxy 172\.30\.81\.2:8080 \{\n        transport http \{\n            keepalive 25s\n        \}\n    \}/u, '    respond "public application"');
    configuration = configuration.replaceAll("/etc/nylzo/monitor-client.pem", "/fixture/client.pem").replaceAll("/var/lib/nylzo-monitor", "/status");
    writeFileSync(join(fixtureDirectory, "Caddyfile"), configuration);
    chmodSync(fixtureDirectory, 0o755);
    for (const file of ["Caddyfile", "client.pem", "status.json"]) chmodSync(join(fixtureDirectory, file), 0o644);
    const compose = readFileSync(resolve("deploy/compose/compose.yaml"), "utf8");
    const image = /image: (caddy:[^\s]+@sha256:[a-f0-9]{64})/u.exec(compose)?.[1];
    assert(image, "The integration test requires the pinned official Caddy image");
    docker("run", "--detach", "--name", name, "--read-only", "--cap-drop", "ALL", "--cap-add", "NET_BIND_SERVICE", "--security-opt", "no-new-privileges:true", "--memory", "128m", "--pids-limit", "64", "--tmpfs", "/data", "--tmpfs", "/config", "--tmpfs", "/status", "--env", "NYLZO_DOMAIN=nylzo.test", "--publish", "127.0.0.1::14443", "--publish", "127.0.0.1::8443", "--mount", `type=bind,source=${fixtureDirectory},target=/fixture,readonly`, image, "caddy", "run", "--config", "/fixture/Caddyfile", "--adapter", "caddyfile");
    const status = (document: unknown) => execFileSync("docker", ["exec", "--interactive", name, "sh", "-ec", "cat > /status/next.json\nchmod 0644 /status/next.json\nmv -f /status/next.json /status/status.json"], { input: JSON.stringify(document), stdio: ["pipe", "pipe", "pipe"], timeout: 10000 });
    status(healthy);
    let ca = "";
    for (let attempt = 0; attempt < 40; attempt++) {
      try { ca = docker("exec", name, "cat", "/data/caddy/pki/authorities/local/root.crt"); break; } catch { await new Promise(resolve => setTimeout(resolve, 250)); }
    }
    const diagnostic = ca ? "" : spawnSync("docker", ["logs", name], { encoding: "utf8", timeout: 10000 });
    assert(ca, typeof diagnostic === "string" ? diagnostic : diagnostic.stdout + diagnostic.stderr);
    const ports = JSON.parse(docker("inspect", name))[0].NetworkSettings.Ports;
    const publicPort = ports["14443/tcp"][0].HostPort;
    const monitorPort = ports["8443/tcp"][0].HostPort;
    const raw = (port: string, identity = {}, path = "/healthz", host = "nylzo.test") => new Promise<{ status: number; text: string }>((resolve, reject) => {
      const outgoing = request({ hostname: "127.0.0.1", port, servername: "nylzo.test", path, headers: { host }, ca, ...identity, agent: false }, response => {
        let text = ""; response.on("data", chunk => text += chunk); response.on("error", reject); response.on("end", () => resolve({ status: response.statusCode!, text }));
      });
      outgoing.setTimeout(10000, () => outgoing.destroy(new Error("TLS fixture timed out")));
      outgoing.on("error", reject); outgoing.end();
    });
    let publicResponse;
    for (let attempt = 0; attempt < 40; attempt++) {
      try { publicResponse = await raw(publicPort); break; } catch { await new Promise(resolve => setTimeout(resolve, 250)); }
    }
    assert.deepEqual(publicResponse, { status: 200, text: "public application" });
    await assert.rejects(raw(monitorPort));
    await assert.rejects(raw(monitorPort, foreign));
    assert.equal((await raw(monitorPort, { cert: publicClient, key }, "/not-a-status-route")).status, 404);
    assert.equal((await raw(monitorPort, { cert: publicClient, key }, "/healthz", "other.test")).status, 421);
    const lookup = (_: string, options: any, callback: any) => options.all ? callback(null, [{ address: "127.0.0.1", family: 4 }]) : callback(null, "127.0.0.1", 4);
    const url = new URL(`https://nylzo.test:${monitorPort}/healthz`);
    await assert.rejects(readHostStatus(url, { cert: publicClient, key }, { lookup }), "The client must verify server trust");
    await assert.rejects(requireClientAuthentication(url, { lookup }), "A server trust failure is not proof of client authentication");
    await requireClientAuthentication(url, { lookup }, ca);
    await assert.rejects(requireClientAuthentication(new URL(`https://nylzo.test:${publicPort}/healthz`), { lookup }, ca));
    const observed = await readHostStatus(url, { cert: publicClient, key, ca }, { lookup });
    validateHostStatus(observed);
    const trustedRoot = join(directory, "server-ca.pem");
    writeFileSync(trustedRoot, ca);
    const cli = () => spawnSync(process.execPath, [resolve("tools/security/monitor-host.mts")], { encoding: "utf8", timeout: 45000, env: { ...process.env, URL: url.toString(), MONITOR_ADDRESS: "127.0.0.1", MONITOR_CERT: publicClient, MONITOR_KEY: key, NODE_EXTRA_CA_CERTS: trustedRoot } });
    assert.equal(cli().status, 0);
    status({ ...healthy, checks: { ...healthy.checks, diskReserve: false } });
    const unhealthyResponse = await raw(monitorPort, { cert: publicClient, key });
    assert.equal(unhealthyResponse.status, 200, JSON.stringify(unhealthyResponse));
    assert.match(unhealthyResponse.text, /"diskReserve":false/u, JSON.stringify(unhealthyResponse));
    const unhealthy = JSON.parse(unhealthyResponse.text);
    assert.throws(() => validateHostStatus(unhealthy));
    status("PRIVATE_RESPONSE_MARKER");
    const refused = cli();
    assert.equal(refused.status, 1);
    assert.doesNotMatch(refused.stdout + refused.stderr, /PRIVATE_RESPONSE_MARKER|BEGIN PRIVATE KEY/u);
    status("x".repeat(9000));
    await assert.rejects(readHostStatus(url, { cert: publicClient, key, ca }, { lookup }), /exceeds its bound/u);
    const logs = spawnSync("docker", ["logs", name], { encoding: "utf8", timeout: 10000 });
    assert.doesNotMatch(logs.stdout + logs.stderr, /TLS handshake error from|remote_ip|remote_addr|BEGIN PRIVATE KEY/u);
  } finally {
    spawnSync("docker", ["rm", "--force", name], { stdio: "ignore", timeout: 30000 });
    rmSync(directory, { recursive: true, force: true });
  }
});
