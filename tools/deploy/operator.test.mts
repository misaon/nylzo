import assert from "node:assert/strict";
import { test, mock } from "node:test";
import { mkdtemp, writeFile, chmod, symlink, rm } from "node:fs/promises";
import { readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { privateFile, inputs, client, credential, initialize, prepare, releaseEvidence } from "./operator.mts";

test("credential loading refuses readable or redirected secrets", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "nylzo-credentials-"));
  try {
    const filename = path.join(directory, "credential");
    await writeFile(filename, "synthetic-test-credential-not-a-live-token", { mode: 0o644 });
    await chmod(filename, 0o644);
    await assert.rejects(privateFile(filename));
    await chmod(filename, 0o600);
    assert.equal(await privateFile(filename), "synthetic-test-credential-not-a-live-token");
    await symlink(filename, path.join(directory, "redirect"));
    await assert.rejects(privateFile(path.join(directory, "redirect")));
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("cloud API calls cannot redirect credentials or reach endpoints supplied by remote data", async () => {
  const seen: { url: string; redirect: string | undefined }[] = [];
  const fetch = mock.method(globalThis, "fetch", async (url: URL, options: RequestInit) => {
    seen.push({ url: url.href, redirect: options.redirect });
    return new Response("{}", { status: 200 });
  });
  try {
    const hetzner = client("synthetic-test-token-not-a-live-token");
    const cloudflare = client("synthetic-test-token-not-a-live-token", "cloudflare");
    for (const endpoint of ["//attacker.example", "/servers/1/../../organizations", "/servers/1?token=anything", "/images/NaN", "/images/1#fragment"]) await assert.rejects(hetzner(endpoint));
    for (const endpoint of ["/zones?name=domain.example&token=anything", "/zones/../accounts", "/zones?name=domain.example\n"]) await assert.rejects(cloudflare(endpoint));
    assert.equal(seen.length, 0);
    await hetzner("/images/123");
    await cloudflare("/zones?name=domain.example");
    assert.deepEqual(seen, [
      { url: "https://api.hetzner.cloud/v1/images/123", redirect: "error" },
      { url: "https://api.cloudflare.com/client/v4/zones?name=domain.example", redirect: "error" },
    ]);
  } finally { fetch.mock.restore(); }
});

test("provisioning installs only a signed host bundle and Caddy build of the initial release", async () => {
  const listing = `${"a".repeat(64)}  nylzo-caddy_0.3.0_linux_amd64.tar.gz\n${"b".repeat(64)}  nylzo-caddy_0.3.0_linux_arm64.tar.gz.sbom.json\n${"c".repeat(64)}  nylzo-host_0.3.0.tar\n`;
  const assets: Record<string, string> = {
    "https://github.com/misaon/nylzo/releases/download/v0.1.5/checksums.txt": `${"a".repeat(64)}  nylzo_0.1.5_linux_amd64.tar.gz\n`,
    "https://github.com/misaon/nylzo/releases/download/v0.2.0/checksums.txt": `${"a".repeat(64)}  nylzo-caddy_0.2.0_linux_amd64.tar.gz\n`,
    "https://github.com/misaon/nylzo/releases/download/v0.3.0/checksums.txt": listing,
    "https://github.com/misaon/nylzo/releases/download/v0.3.0/checksums.txt.sigstore.json": "{}",
  };
  const fetch = mock.method(globalThis, "fetch", async (url: string) => {
    const body = assets[url];
    return body === undefined ? new Response("", { status: 404 }) : new Response(body, { status: 200 });
  });
  const verified: string[] = [];
  const signed = (release: string, checksums: string) => { verified.push(release); assert.equal(readFileSync(checksums, "utf8"), listing); };
  try {
    assert.deepEqual(await releaseEvidence("v0.3.0", "amd64", signed), { url: "https://github.com/misaon/nylzo/releases/download/v0.3.0/nylzo-host_0.3.0.tar", sha256: "c".repeat(64) });
    assert.deepEqual(verified, ["v0.3.0"]);
    await assert.rejects(releaseEvidence("v0.3.0", "amd64", () => { throw new Error("signature mismatch"); }), /signature mismatch/u);
    await assert.rejects(releaseEvidence("v0.3.0", "arm64", signed), /ships no Caddy build/u);
    await assert.rejects(releaseEvidence("v0.2.0", "amd64", signed), /ships no host bundle/u);
    await assert.rejects(releaseEvidence("v0.1.5", "amd64", signed), /ships no Caddy build/u);
    await assert.rejects(releaseEvidence("v9.9.9", "amd64", signed), /Cannot read the checksums/u);
    assert.deepEqual(verified, ["v0.3.0"], "Listing checks come before any signature check");
  } finally { fetch.mock.restore(); }
});

test("cloud credentials can come from a password manager command instead of a file", async () => {
  const saved = { ...process.env };
  try {
    delete process.env.HCLOUD_TOKEN;
    process.env.HCLOUD_TOKEN_COMMAND = "printf '%s\\n' synthetic-test-credential-from-a-command";
    assert.equal(await credential("HCLOUD_TOKEN"), "synthetic-test-credential-from-a-command");
    process.env.HCLOUD_TOKEN_COMMAND = "exit 3";
    await assert.rejects(credential("HCLOUD_TOKEN"));
  } finally { process.env = saved; }
});

test("planning stops before any cloud call without a Caddy build, or without a hardware key for a server", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "nylzo-site-"));
  const seen: string[] = [];
  const fetch = mock.method(globalThis, "fetch", async (url: string) => {
    seen.push(url);
    return new Response(`${"a".repeat(64)}  nylzo_0.1.5_linux_amd64.tar.gz\n`, { status: 200 });
  });
  try {
    await chmod(directory, 0o700);
    const siteFile = path.join(directory, "site.json");
    await writeFile(siteFile, JSON.stringify({
      name: "nylzo-test", domain: "nylzo.test-instance.dev", location: "nbg1", serverType: "cx23", architecture: "amd64",
      administratorNetworks: ["10.1.2.3/32"],
      administratorPublicKey: "ssh-ed25519 " + Buffer.concat([Buffer.from("0000000b7373682d6564323535313900000020", "hex"), Buffer.alloc(32, 1)]).toString("base64"),
      reportPublicKey: Buffer.alloc(1216, 1).toString("base64url"),
      operator: "Test operator", contact: "security@nylzo.test-instance.dev", hosting: "Test provider",
      dailyCreates: 250, initialRelease: "v0.1.5", automaticUpdates: true,
    }));
    await assert.rejects(prepare(siteFile, "image"), /ships no Caddy build/u);
    await assert.rejects(prepare(siteFile, "server"), /hardware-backed sk-ssh-ed25519@openssh\.com/u);
    assert.deepEqual(seen, ["https://github.com/misaon/nylzo/releases/download/v0.1.5/checksums.txt"]);
  } finally {
    fetch.mock.restore();
    await rm(directory, { recursive: true, force: true });
  }
});

test("reviewed plans are bound to provider locks, infrastructure, variables and generated host configuration", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "nylzo-plan-"));
  try {
    for (const name of ["main.tf", ".terraform.lock.hcl", "deployment.auto.tfvars.json", "user-data"]) await writeFile(path.join(directory, name), "initial");
    let previous = await inputs(directory);
    for (const name of ["main.tf", ".terraform.lock.hcl", "deployment.auto.tfvars.json", "user-data"]) {
      await writeFile(path.join(directory, name), "changed");
      const next = await inputs(directory);
      assert.notEqual(next, previous);
      previous = next;
    }
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("initialization refuses to create private keys in a directory that Git can track", async () => {
  await assert.rejects(initialize("unused-public-key", "tools/deploy/operator-private-keys"), /Keep operator keys outside/u);
});
