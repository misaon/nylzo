import assert from "node:assert/strict";
import { test } from "node:test";
import { administratorKeyIsHardware, configuration, patchCandidate } from "./configuration.mts";
import { releaseBundle, render } from "./render.mts";
import { hostSources } from "./bundle.mts";
import { gunzipSync } from "node:zlib";
import { readFile } from "node:fs/promises";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";

const ed25519 = "ssh-ed25519 " + Buffer.concat([Buffer.from("0000000b7373682d6564323535313900000020", "hex"), Buffer.alloc(32, 1)]).toString("base64");
const field = (value: Buffer | string) => { const bytes = Buffer.from(value); const length = Buffer.alloc(4); length.writeUInt32BE(bytes.length); return Buffer.concat([length, bytes]); };
const securityKey = (application = "ssh:", key = Buffer.alloc(32, 2), type = "sk-ssh-ed25519@openssh.com") => `${type} ${Buffer.concat([field(type), field(key), field(application)]).toString("base64")} operator@laptop`;
const site = {
  name: "nylzo-test", domain: "nylzo.example.org", location: "nbg1", serverType: "cx23", architecture: "amd64",
  administratorNetworks: ["192.0.2.1/32", "2001:db8::1/128"],
  administratorPublicKey: ed25519,
  reportPublicKey: Buffer.alloc(1216, 1).toString("base64url"),
  operator: "Test operator", contact: "security@example.org", hosting: "Test provider", dailyCreates: 250, initialRelease: "v0.1.2", automaticUpdates: true,
};
const bundle = releaseBundle("v0.1.2", "c".repeat(64));
const decode = (ignition: { storage: { files: any[] } }) => new Map(ignition.storage.files.filter(file => file.contents.source.startsWith("data:")).map(file => [file.path, gunzipSync(Buffer.from(file.contents.source.split(",")[1]!, "base64")).toString()]));

test("deployment input cannot inject services, expose SSH globally, or select a mismatched architecture", () => {
  for (const changed of [
    { domain: "example.org\nExecStart=/bin/false" }, { name: "../other" },
    { administratorNetworks: ["0.0.0.0/0"] }, { administratorNetworks: ["::/0"] },
    { administratorNetworks: ["192.0.2.1/32/anything"] }, { architecture: "arm64" },
    { dailyCreates: 2000 }, { initialRelease: "v0.1.2;false" }, { reportPublicKey: "invalid" }, { publicAccess: "true" },
    { operator: "name\nNYLZO_TRUSTED_PROXIES=0.0.0.0/0" }, { unexpectedRootCommand: "/bin/false" },
    { administratorPublicKey: "ssh-ed25519 " + Buffer.alloc(51).toString("base64") },
    { administratorPublicKey: securityKey("web:") }, { administratorPublicKey: securityKey("ssh:", Buffer.alloc(31, 2)) },
    { administratorPublicKey: securityKey("ssh:", Buffer.alloc(32, 2), "sk-ecdsa-sha2-nistp256@openssh.com") },
    { administratorPublicKey: securityKey() + "\nssh-ed25519 AAAA" },
    { heartbeatUrl: "http://heartbeat.example.org/ping/x" }, { heartbeatUrl: "https://heartbeat.example.org/ping?x=1" },
    { tang: { threshold: 1, servers: [{ url: "http://tang.example.org", thumbprint: "A".repeat(43) }] } },
    { tang: { threshold: 3, servers: [{ url: "http://tang.example.org", thumbprint: "A".repeat(43) }, { url: "http://tang.example.net", thumbprint: "B".repeat(43) }] } },
    { tang: { threshold: 2, servers: [{ url: "http://tang.example.org/path", thumbprint: "A".repeat(43) }, { url: "http://tang.example.net", thumbprint: "B".repeat(43) }] } },
    { acmeAccountUri: "https://acme.example.org/acme/acct/1" },
  ]) assert.throws(() => configuration({ ...site, ...changed }), JSON.stringify(changed));
  assert.equal(configuration(site), site);
  assert.equal(administratorKeyIsHardware(securityKey()), true);
  assert.equal(administratorKeyIsHardware(securityKey("ssh:nylzo")), true);
  assert.equal(administratorKeyIsHardware(ed25519), false);
  configuration({ ...site, administratorPublicKey: securityKey(), heartbeatUrl: "https://hc-ping.com/0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d", acmeAccountUri: "https://acme-v02.api.letsencrypt.org/acme/acct/3938438202",
    tang: { threshold: 2, servers: [{ url: "http://tang.example.org", thumbprint: "A".repeat(43) }, { url: "https://tang.example.net:7500", thumbprint: "B".repeat(43) }] } });
});

test("automatic patch selection refuses prereleases, downgrades, breaking upgrades and overflowing versions", () => {
  assert(patchCandidate("v0.1.2", "v0.1.3"));
  for (const version of ["v0.1.1", "v0.1.2", "v0.2.0", "v1.1.3", "v0.1.3-beta", "v0.1.03", "v0.1.9999999999999999999999999", "v0.1.3\nanything"]) assert(!patchCandidate("v0.1.2", version));
});

test("server user data stays small, carries no secret, and installs only the signed host bundle of the initial release", async () => {
  const certificate = "-----BEGIN CERTIFICATE-----\n" + "A".repeat(1800) + "\n-----END CERTIFICATE-----\n";
  const largest = { ...site, administratorPublicKey: securityKey(), heartbeatUrl: "https://hc-ping.com/0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d",
    tang: { threshold: 2, servers: [{ url: "http://tang.example.org", thumbprint: "A".repeat(43) }, { url: "http://tang.example.net", thumbprint: "B".repeat(43) }] } };
  const { ignition, cloud } = await render(largest, bundle);
  assert(Buffer.byteLength(JSON.stringify(ignition)) + certificate.length * 1.4 <= 24576, "Server user data must leave room within Hetzner's 32 KiB limit");
  assert(Buffer.byteLength(cloud) <= 32768);
  const factory = JSON.parse(cloud.slice("#cloud-config\n".length));
  assert(factory.write_files.every((file: { path: string }) => file.path.startsWith("/var/tmp/nylzo-image/")), "Large boot artifacts must not fill Ubuntu's small /run tmpfs");
  const bootstrap = factory.write_files.find((file: { path: string }) => file.path.endsWith("/boot"));
  assert.match(gunzipSync(Buffer.from(bootstrap.content, "base64")).toString(), /cd \/var\/tmp\/nylzo-image/u);
  const remote = ignition.storage.files.find(file => file.path === "/opt/nylzo/host/initial.tar")!;
  assert.deepEqual(remote.contents, { source: "https://github.com/misaon/nylzo/releases/download/v0.1.2/nylzo-host_0.1.2.tar", verification: { hash: `sha256-${"c".repeat(64)}` } });
  const files = decode(ignition);
  assert(!JSON.stringify(ignition).includes("HCLOUD_TOKEN"));
  assert(![...files.keys()].some(name => /pepper$|report-key|state-encryption/u.test(name)));
  assert.match(files.get("/etc/ssh/sshd_config.d/20-nylzo.conf")!, /^KexAlgorithms mlkem768x25519-sha256,sntrup761x25519-sha512$/mu);
  assert.match(files.get("/etc/sysctl.d/90-nylzo.conf")!, /^kernel.kexec_load_disabled=1$/mu);
  assert.match(files.get("/etc/systemd/coredump.conf.d/20-nylzo.conf")!, /^Storage=none$/mu);
  assert.match(files.get("/etc/selinux/config")!, /^SELINUX=enforcing$/mu);
  assert.match(files.get("/etc/nylzo/service.env")!, /NYLZO_TRUSTED_PROXIES=172\.30\.81\.1\/32/u);
  assert.equal(files.get("/etc/nylzo/host.env"), "AUTOMATIC_UPDATES=true\nPUBLIC_ACCESS=false\nWWW_REDIRECT=false\nADMINISTRATOR_NETWORKS=192.0.2.1/32,2001:db8::1/128\n");
  assert.equal(JSON.parse(files.get("/etc/nylzo/tang.json")!).t, 2);
  assert.equal(ignition.storage.files.find(file => file.path === "/etc/nylzo/heartbeat.url")!.mode, 0o600);
  assert(!ignition.passwd.users.some(user => "sshAuthorizedKeys" in user && user.name !== "core"));
  assert.deepEqual(ignition.systemd.units.filter(unit => !("mask" in unit)).map(unit => unit.name), ["nylzo-host.service"]);
  const first = ignition.systemd.units.find(unit => unit.name === "nylzo-host.service")!;
  assert.match(first.contents, /^Before=sysinit\.target network-pre\.target shutdown\.target$/mu);
  assert.match(first.contents, /activate --first-boot \/opt\/nylzo\/host\/initial$/mu);
  const apex = decode((await render({ ...site, dnsZone: site.domain }, bundle)).ignition);
  assert.match(apex.get("/etc/nylzo/host.env")!, /^WWW_REDIRECT=true$/mu);
  assert(!decode((await render(site, bundle)).ignition).has("/etc/nylzo/tang.json"));
});

test("the host bundle isolates the pepper, the probe and the deployment credentials", async () => {
  const sources = await hostSources();
  assert.match(sources.get("application/run")!, /\/run\/nylzo-pepper:ro,Z/u);
  assert.match(sources.get("application/run")!, /--ulimit memlock=1073741824:1073741824/u);
  assert(!sources.get("application/run")!.includes("--publish"));
  assert.match(sources.get("monitoring/host-policy")!, /SELinux enforcement is required/u);
  assert.match(sources.get("application/prepare")!, /systemd-sysctl \/etc\/sysctl\.d\/90-nylzo\.conf/u);
  assert.match(sources.get("application/health")!, /\/opt\/nylzo\/runtime\/host-policy/u);
  assert.match(sources.get("proxy/Caddyfile")!, /reverse_proxy 172\.30\.81\.2:8080/u);
  assert(!sources.get("update/update")!.includes("gh api"));
  assert.match(sources.get("update/update")!, /--network none/u);
  assert.match(sources.get("update/update-engine.conf")!, /Requires=nylzo-ready\.service/u);
  assert.match(sources.get("system/daemon.json")!, /"userns-remap":"dockremap"/u);
  assert.match(sources.get("system/containerd.toml")!, /disabled_plugins = \["io\.containerd\.grpc\.v1\.cri"/u);
  const identity = sources.get("system/nylzo-ssh-identity.service")!;
  for (const expected of ["Requires=sshd-keygen.service", "-f /etc/ssh/ssh_host_ed25519_key.pub", "TTYPath=/dev/tty1"]) assert(identity.includes(expected));
  const ssh = sources.get("system/sshd.conf")!;
  for (const expected of ["PubkeyAcceptedAlgorithms sk-ssh-ed25519@openssh.com,ssh-ed25519", "PubkeyAuthOptions verify-required", "HostKeyAlgorithms ssh-ed25519", "Ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com,aes128-gcm@openssh.com", "MACs hmac-sha2-512-etm@openssh.com,hmac-sha2-256-etm@openssh.com"]) assert(ssh.includes(`${expected}\n`), expected);
});

test("public services require the shared traffic ceiling and DNS provisioning works on Cloudflare Free", async () => {
  const sources = await hostSources();
  assert.match(sources.get("units")!, /^enable always nylzo-bandwidth\.service$/mu);
  assert(!sources.get("system/nylzo-bandwidth.service")!.includes("Requires=nylzo-tools.service"));
  for (const name of ["update/nylzo-tools.service", "application/nylzo-initialize.service", "application/nylzo.service", "proxy/caddy.service"]) {
    assert.match(sources.get(name)!, /Requires=.*nylzo-bandwidth.service/u, name);
    assert.match(sources.get(name)!, /After=.*nylzo-bandwidth.service/u, name);
  }
  // Both address families share one NIC. Even continuous use leaves a margin
  // below the included allowance, including a maximum-length calendar month.
  const script = sources.get("system/bandwidth")!;
  assert(!script.includes("jq"), "The first-boot ceiling must not depend on downloaded tools");
  const rate = /tbf rate ([0-9]+)mbit/u.exec(script);
  assert(rate);
  assert(Number(rate[1]) * 1_000_000 / 8 * 31 * 86400 < 20_000_000_000_000);
  const dns = await readFile(new URL("../../deploy/hetzner-flatcar/infrastructure/dns/main.tf", import.meta.url), "utf8");
  assert.match(dns, /proxied\s*=\s*false/u);
  assert(!dns.includes("zone_mode"), "Cloudflare Free can refuse account-wide custom DNS modes");
});

test("DNS allows only the instance's Let's Encrypt account by HTTP-01, no wildcards, no mail and no Cloudflare-held certificates", async () => {
  const dns = await readFile(new URL("../../deploy/hetzner-flatcar/infrastructure/dns/main.tf", import.meta.url), "utf8");
  assert.match(dns, /resource "cloudflare_universal_ssl_setting" "zone" \{\n {2}zone_id = cloudflare_zone\.zone\.id\n {2}enabled = false\n\}/u);
  assert.match(dns, /"validationmethods=http-01",\n\s+var\.acme_account_uri == "" \? "" : "accounturi=\$\{var\.acme_account_uri\}",/u);
  assert.match(dns, /tag {3}= "issuewild"\n\s+value = ";"/u);
  assert.match(dns, /type {5}= "MX"\n\s+content {2}= "\."\n\s+priority = 0/u);
  assert.match(dns, /content = "\\"v=spf1 -all\\""/u);
  assert.match(dns, /content = "\\"v=DMARC1; p=reject; sp=reject; adkim=s; aspf=s\\""/u);
  assert.match(dns, /count {3}= local\.apex \? 1 : 0/u);
  const caddy = (await hostSources()).get("proxy/Caddyfile")!;
  assert.match(caddy, /disable_tlsalpn_challenge/u);
});

test("host monitoring is opt-in and embeds only its pinned public client identity", async () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-monitor-config-"));
  try {
    execFileSync("go", ["run", "./tools/deploy/monitor-identity", join(directory, "identity")], { stdio: "pipe" });
    const certificate = readFileSync(join(directory, "identity/client.pem"), "utf8");
    const key = readFileSync(join(directory, "identity/client-key.pem"), "utf8");
    for (const invalid of [key, certificate + key, certificate + certificate]) await assert.rejects(render({ ...site, monitorClientCertificate: invalid }, bundle));
    assert.throws(() => configuration({ ...site, automaticUpdates: false, monitorClientCertificate: certificate }));
    const { ignition } = await render({ ...site, monitorClientCertificate: certificate }, bundle);
    const files = decode(ignition);
    assert.equal(files.get("/etc/nylzo/monitor-client.pem"), certificate);
    assert(!JSON.stringify(ignition).includes("PRIVATE KEY"));
    assert(![...files.values()].some(value => value.includes(key)));
    assert(!decode((await render(site, bundle)).ignition).has("/etc/nylzo/monitor-client.pem"));
    const monitor = (await hostSources()).get("proxy/monitor.Caddyfile")!;
    for (const expected of [":8443", "mode require_and_verify", "verifier leaf file", "protocols tls1.3", "curves x25519mlkem768"]) assert(monitor.includes(expected), expected);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
