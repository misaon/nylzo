import assert from "node:assert/strict";
import { test } from "node:test";
import { configuration, patchCandidate } from "./configuration.mts";
import { render } from "./render.mts";
import { gunzipSync } from "node:zlib";

const site = {
  name: "nylzo-test", domain: "nylzo.example.org", location: "nbg1", serverType: "cx23", architecture: "amd64",
  administratorNetworks: ["192.0.2.1/32", "2001:db8::1/128"],
  administratorPublicKey: "ssh-ed25519 " + Buffer.concat([Buffer.from("0000000b7373682d6564323535313900000020", "hex"), Buffer.alloc(32, 1)]).toString("base64"),
  reportPublicKey: Buffer.alloc(1216, 1).toString("base64url"),
  operator: "Test operator", contact: "security@example.org", hosting: "Test provider", dailyCreates: 250, initialRelease: "v0.1.2", automaticUpdates: true,
};

test("deployment input cannot inject services, expose SSH globally, or select a mismatched architecture", () => {
  for (const changed of [
    { domain: "example.org\nExecStart=/bin/false" }, { name: "../other" },
    { administratorNetworks: ["0.0.0.0/0"] }, { administratorNetworks: ["::/0"] },
    { administratorNetworks: ["192.0.2.1/32/anything"] }, { architecture: "arm64" },
    { dailyCreates: 2000 }, { initialRelease: "v0.1.2;false" }, { reportPublicKey: "invalid" },
    { operator: "name\nNYLZO_TRUSTED_PROXIES=0.0.0.0/0" }, { unexpectedRootCommand: "/bin/false" },
    { administratorPublicKey: "ssh-ed25519 " + Buffer.alloc(51).toString("base64") },
  ]) assert.throws(() => configuration({ ...site, ...changed }));
  assert.equal(configuration(site), site);
});

test("automatic patch selection refuses prereleases, downgrades, breaking upgrades and overflowing versions", () => {
  assert(patchCandidate("v0.1.2", "v0.1.3"));
  for (const version of ["v0.1.1", "v0.1.2", "v0.2.0", "v1.1.3", "v0.1.3-beta", "v0.1.03", "v0.1.9999999999999999999999999", "v0.1.3\nanything"]) assert(!patchCandidate("v0.1.2", version));
});

test("generated hosts isolate deployment credentials, peppers, reporting keys and container authority", async () => {
  const { ignition, cloud } = await render(site);
  assert(Buffer.byteLength(JSON.stringify(ignition)) <= 32768);
  assert(Buffer.byteLength(cloud) <= 32768);
  const files = new Map(ignition.storage.files.map(file => [file.path, gunzipSync(Buffer.from(file.contents.source.split(",")[1]!, "base64")).toString()]));
  assert(!JSON.stringify(ignition).includes("HCLOUD_TOKEN"));
  assert(![...files.keys()].some(name => /pepper$|report-key|state-encryption/u.test(name)));
  assert.match(files.get("/etc/ssh/sshd_config.d/20-nylzo.conf")!, /PermitRootLogin no/u);
  assert.match(files.get("/etc/docker/daemon.json")!, /"userns-remap":"dockremap"/u);
  assert(!files.get("/opt/nylzo/runtime/run")!.includes("--publish"));
  assert.match(files.get("/etc/caddy/Caddyfile")!, /reverse_proxy 172\.30\.81\.2:8080/u);
  assert.match(files.get("/etc/nylzo/service.env")!, /NYLZO_TRUSTED_PROXIES=172\.30\.81\.1\/32/u);
  assert(!files.get("/opt/nylzo/runtime/update")!.includes("gh api"));
  assert(!ignition.passwd.users.some(user => "sshAuthorizedKeys" in user && user.name !== "core"));
  const updateEngine = ignition.systemd.units.find(unit => unit.name === "update-engine.service")!;
  assert("dropins" in updateEngine && updateEngine.dropins[0]!.contents.includes("Requires=nylzo-ready.service"));
  assert.match(files.get("/opt/nylzo/runtime/update")!, /--network none/u);
});
