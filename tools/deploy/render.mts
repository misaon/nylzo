import { readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import assert from "node:assert/strict";
import { configuration } from "./configuration.mts";
import { archive, hostSources } from "./bundle.mts";

export const profile = fileURLToPath(new URL("../../deploy/hetzner-flatcar/", import.meta.url));
const read = (filename: string) => readFile(path.join(profile, filename), "utf8");
const embedded = (filename: string, contents: string | Buffer, mode = 0o644) => ({ path: filename, mode, overwrite: true, contents: { compression: "gzip", source: `data:;base64,${gzipSync(contents, { level: 9 }).toString("base64")}` } });
const remote = (filename: string, bundle: { url: string; sha256: string }) => {
  assert.match(bundle.url, /^https:\/\/github\.com\/misaon\/nylzo\/releases\/download\/v[0-9]+\.[0-9]+\.[0-9]+\/nylzo-host_[0-9]+\.[0-9]+\.[0-9]+\.tar$/u);
  assert.match(bundle.sha256, /^[a-f0-9]{64}$/u);
  return { path: filename, mode: 0o600, overwrite: true, contents: { source: bundle.url, verification: { hash: `sha256-${bundle.sha256}` } } };
};
const service = (name: string, contents: string, enabled = false) => ({ name, contents, enabled });
export const key = await read("bootstrap/flatcar.asc");
export const toolchain = JSON.parse(await read("toolchain.json"));
const early = ["/etc/ssh/sshd_config.d/20-nylzo.conf", "/etc/sysctl.d/90-nylzo.conf", "/etc/systemd/journald.conf.d/20-nylzo.conf", "/etc/systemd/coredump.conf.d/20-nylzo.conf"];
const firstBoot = `[Unit]
Description=Install the host configuration of this Nylzo release
DefaultDependencies=no
After=local-fs.target
Before=sysinit.target network-pre.target shutdown.target
Wants=network-pre.target
Conflicts=shutdown.target
ConditionPathExists=!/opt/nylzo/host/current
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/mkdir -p /opt/nylzo/host/initial
ExecStart=/usr/bin/tar --extract --file /opt/nylzo/host/initial.tar --directory /opt/nylzo/host/initial --no-same-owner --no-same-permissions
ExecStart=/usr/bin/bash /opt/nylzo/host/initial/update/activate --first-boot /opt/nylzo/host/initial
[Install]
WantedBy=sysinit.target
`;

export type HostBundle = { url: string; sha256: string } | Map<string, string>;

export const releaseBundle = (release: string, sha256: string) => ({ url: `https://github.com/misaon/nylzo/releases/download/${release}/nylzo-host_${release.slice(1)}.tar`, sha256 });

export async function render(input: unknown, bundle: HostBundle) {
  const site = configuration(input);
  const host = bundle instanceof Map ? bundle : await hostSources();
  const tools = [
    `ARCHITECTURE=${site.architecture}`,
    ...["gh", "cosign", "jq"].flatMap(name => [`${name.toUpperCase()}_VERSION=${toolchain[name].version}`, `${name.toUpperCase()}_SHA256=${toolchain[name][site.architecture]}`]),
  ].join("\n") + "\n";
  const environment = Object.entries({
    NYLZO_LISTEN: ":8080", NYLZO_DATA_DIR: "/home/nonroot", NYLZO_PEPPER_FILE: "/run/nylzo-pepper",
    NYLZO_REPORT_KEY: site.reportPublicKey, NYLZO_TRUSTED_PROXIES: "172.30.81.1/32",
    NYLZO_GLOBAL_DAILY_CREATES: site.dailyCreates, NYLZO_OPERATOR: site.operator,
    NYLZO_MIN_FREE_BYTES: 4 * 1024 ** 3,
    NYLZO_CONTACT: site.contact, NYLZO_HOSTING: site.hosting, NYLZO_PUBLIC_URL: `https://${site.domain}`,
  }).map(([name, value]) => `${name}=${value}`).join("\n") + "\n";
  const settings = Object.entries({
    AUTOMATIC_UPDATES: site.automaticUpdates, PUBLIC_ACCESS: site.publicAccess ?? false,
    WWW_REDIRECT: site.dnsZone === site.domain, ADMINISTRATOR_NETWORKS: site.administratorNetworks.map((network: string) => network.toLowerCase()).join(","),
  }).map(([name, value]) => `${name}=${value}`).join("\n") + "\n";
  const manifest = new Map(host.get("manifest")!.trimEnd().split("\n").slice(1).map(line => line.split(" ") as [string, string]));
  const files: any[] = [
    embedded("/etc/nylzo/tools.env", tools),
    embedded("/etc/nylzo/site.json", JSON.stringify({ architecture: site.architecture, domain: site.domain, initialRelease: site.initialRelease })),
    embedded("/etc/nylzo/service.env", environment),
    embedded("/etc/nylzo/caddy.env", `NYLZO_DOMAIN=${site.domain}\n`),
    embedded("/etc/nylzo/host.env", settings),
    embedded("/etc/subuid", "dockremap:100000:65536\n"),
    embedded("/etc/subgid", "dockremap:100000:65536\n"),
    embedded("/etc/selinux/config", "SELINUX=enforcing\nSELINUXTYPE=mcs\n"),
    ...early.map(target => embedded(target, host.get(manifest.get(target)!)!)),
    bundle instanceof Map ? embedded("/opt/nylzo/host/initial.tar", archive(bundle), 0o600) : remote("/opt/nylzo/host/initial.tar", bundle),
  ];
  if (site.monitorClientCertificate) files.push(embedded("/etc/nylzo/monitor-client.pem", site.monitorClientCertificate));
  if (site.heartbeatUrl) files.push(embedded("/etc/nylzo/heartbeat.url", `${site.heartbeatUrl}\n`, 0o600));
  if (site.tang) files.push(embedded("/etc/nylzo/tang.json", JSON.stringify({ t: site.tang.threshold, pins: { tang: site.tang.servers.map((server: { url: string; thumbprint: string }) => ({ url: server.url, thp: server.thumbprint })) } }), 0o600));
  const ignition = {
    ignition: { version: "3.4.0" },
    passwd: { groups: [{ name: "dockremap", gid: 2000 }, { name: "caddy", gid: 2001 }], users: [
      { name: "root", passwordHash: "*" }, { name: "core", passwordHash: "*", sshAuthorizedKeys: [site.administratorPublicKey] },
      { name: "dockremap", uid: 2000, primaryGroup: "dockremap", noCreateHome: true, shell: "/sbin/nologin" },
      { name: "caddy", uid: 2001, primaryGroup: "caddy", noCreateHome: true, shell: "/sbin/nologin" },
    ] }, storage: { files }, systemd: { units: [
      { name: "serial-getty@ttyS0.service", mask: true }, { name: "getty@tty1.service", mask: true }, service("nylzo-host.service", firstBoot, true),
    ] as any[] },
  };
  const board = site.architecture === "amd64" ? "amd64-usr" : "arm64-usr";
  const installer = {
    ignition: { version: "3.4.0" },
    storage: { files: [embedded("/opt/nylzo-image/flatcar.asc", key), embedded("/opt/nylzo-image/install", await read("bootstrap/install"), 0o700)] },
    systemd: { units: [service("nylzo-image-install.service", `[Unit]\nWants=network-online.target\nAfter=network-online.target\n[Service]\nType=oneshot\nEnvironment=FLATCAR_BOARD=${board}\nEnvironment=FLATCAR_VERSION=${toolchain.flatcar}\nEnvironment=EXPECTED_BUILDER_NAME=${site.name}-image-builder\nExecStart=/opt/nylzo-image/install\nTimeoutStartSec=20min\n[Install]\nWantedBy=multi-user.target\n`, true)] },
  };
  installer.systemd.units.push({ name: "update-engine.service", mask: true } as any, { name: "locksmithd.service", mask: true } as any, { name: "sshd.service", mask: true } as any, { name: "sshd.socket", mask: true } as any);
  const cloudInstaller = structuredClone(installer);
  cloudInstaller.storage.files[0]!.contents.source = "@@FLATCAR_IMAGE_KEY@@";
  const cloud = "#cloud-config\n" + JSON.stringify({
    ssh_pwauth: false,
    write_files: [
      ["flatcar.asc", key], ["installer.ign", JSON.stringify(cloudInstaller)], ["boot", await read("bootstrap/boot")],
      ["env", `FLATCAR_BOARD=${board}\nFLATCAR_VERSION=${toolchain.flatcar}\nEXPECTED_BUILDER_NAME=${site.name}-image-builder\n`],
    ].map(([name, contents]) => ({ path: `/var/tmp/nylzo-image/${name}`, encoding: "gz+b64", content: gzipSync(contents!).toString("base64"), permissions: "0600" })),
    runcmd: [["/bin/bash", "-c", "set -a; . /var/tmp/nylzo-image/env; exec /bin/bash /var/tmp/nylzo-image/boot"]],
  });
  assert(Buffer.byteLength(cloud) <= 32768, "Image factory user_data exceeds 32 KiB");
  assert(bundle instanceof Map || Buffer.byteLength(JSON.stringify(ignition)) <= 32768, "Server user_data exceeds 32 KiB");
  return { site, ignition, installer, cloud };
}

if (import.meta.main) {
  const result = await render(JSON.parse(await readFile(process.argv[2]!, "utf8")), await hostSources());
  const destination = path.resolve(process.argv[3] ?? ".deploy/rendered");
  await mkdir(destination, { recursive: true, mode: 0o700 });
  for (const [name, contents] of Object.entries({ "server.ign": JSON.stringify(result.ignition), "installer.ign": JSON.stringify(result.installer), "image.cloud-config": result.cloud })) await writeFile(path.join(destination, name), contents, { mode: 0o600 });
  console.log(`Rendered image factory and server configuration in ${destination}`);
}
