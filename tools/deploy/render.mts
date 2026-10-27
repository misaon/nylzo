import { readFile, writeFile, mkdir, readdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import assert from "node:assert/strict";
import { configuration } from "./configuration.mts";

export const profile = fileURLToPath(new URL("../../deploy/hetzner-flatcar/", import.meta.url));
const read = (filename: string) => readFile(path.join(profile, filename), "utf8");
const embedded = (filename: string, contents: string, mode = 0o644) => ({ path: filename, mode, overwrite: true, contents: { compression: "gzip", source: `data:;base64,${gzipSync(contents).toString("base64")}` } });
const service = (name: string, contents: string, enabled = false) => ({ name, contents, enabled });
export const key = await read("bootstrap/flatcar.asc");
export const toolchain = JSON.parse(await read("toolchain.json"));

export async function render(input: unknown) {
  const site = configuration(input);
  const files: any[] = [];
  for (const filename of await readdir(path.join(profile, "runtime"))) {
    if (filename !== "Caddyfile") files.push(embedded(`/opt/nylzo/runtime/${filename}`, await read(`runtime/${filename}`), 0o755));
  }
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
  files.push(
    embedded("/etc/nylzo/tools.env", tools),
    embedded("/etc/nylzo/site.json", JSON.stringify({ architecture: site.architecture, domain: site.domain, initialRelease: site.initialRelease, caddy: toolchain.caddy })),
    embedded("/etc/nylzo/service.env", environment),
    embedded("/etc/nylzo/caddy.env", `NYLZO_DOMAIN=${site.domain}\n`),
    embedded("/etc/caddy/Caddyfile", await read("runtime/Caddyfile")),
    embedded("/etc/subuid", "dockremap:100000:65536\n"),
    embedded("/etc/subgid", "dockremap:100000:65536\n"),
    embedded("/etc/docker/daemon.json", JSON.stringify({ "userns-remap": "dockremap", "no-new-privileges": true, "log-driver": "local", "log-opts": { "max-size": "10m", "max-file": "3" } })),
    embedded("/etc/ssh/sshd_config.d/20-nylzo.conf", "PermitRootLogin no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nAuthenticationMethods publickey\nAllowUsers core\nAllowAgentForwarding no\nAllowTcpForwarding no\nX11Forwarding no\nPermitTunnel no\n"),
    embedded("/etc/flatcar/update.conf", "GROUP=stable\nREBOOT_STRATEGY=reboot\nLOCKSMITHD_REBOOT_WINDOW_START=03:00\nLOCKSMITHD_REBOOT_WINDOW_LENGTH=1h\n"),
    embedded("/etc/systemd/journald.conf.d/20-nylzo.conf", "[Journal]\nSystemMaxUse=128M\nRuntimeMaxUse=32M\nMaxRetentionSec=7day\n"),
  );
  const units = [
    { name: "serial-getty@ttyS0.service", mask: true },
    { name: "getty@tty1.service", mask: true },
    service("nylzo-tools.service", "[Unit]\nWants=network-online.target\nAfter=network-online.target\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/opt/nylzo/runtime/install-tools\n"),
    service("nylzo-initialize.service", "[Unit]\nRequires=nylzo-tools.service docker.service\nAfter=nylzo-tools.service docker.service network-online.target\n[Service]\nType=oneshot\nRemainAfterExit=yes\nTimeoutStartSec=15min\nExecStart=/opt/nylzo/runtime/prepare\nExecStart=/opt/nylzo/runtime/update initialize\n[Install]\nWantedBy=multi-user.target\n", true),
    service("nylzo.service", "[Unit]\nRequires=docker.service\nAfter=docker.service\nStartLimitIntervalSec=60\nStartLimitBurst=5\n[Service]\nExecStart=/opt/nylzo/runtime/run\nExecStop=/usr/bin/docker stop --time 30 nylzo\nRestart=on-failure\nRestartSec=5\nTimeoutStopSec=45\n"),
    service("caddy.service", "[Unit]\nAfter=network-online.target\nWants=network-online.target\n[Service]\nUser=caddy\nGroup=caddy\nEnvironmentFile=/etc/nylzo/caddy.env\nEnvironment=XDG_DATA_HOME=/var/lib/caddy\nEnvironment=XDG_CONFIG_HOME=/var/lib/caddy/config\nExecStart=/opt/nylzo/bin/caddy run --config /etc/caddy/Caddyfile\nRestart=on-failure\nRestartSec=5\nStateDirectory=caddy\nAmbientCapabilities=CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_BIND_SERVICE\nNoNewPrivileges=true\nProtectSystem=strict\nProtectHome=true\nPrivateTmp=true\nPrivateDevices=true\nProtectKernelTunables=true\nProtectKernelModules=true\nProtectControlGroups=true\nRestrictSUIDSGID=true\nLockPersonality=true\nRestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX\nMemoryMax=256M\nTasksMax=128\n"),
    service("nylzo-ready.service", "[Unit]\nRequires=nylzo-initialize.service\nAfter=nylzo-initialize.service\nOnFailure=nylzo-recover-os.service\n[Service]\nType=oneshot\nRemainAfterExit=yes\nTimeoutStartSec=3min\nExecStart=/opt/nylzo/runtime/health\n"),
    service("nylzo-recover-os.service", "[Service]\nType=oneshot\nExecStart=/opt/nylzo/runtime/recover-os\n"),
    { name: "update-engine.service", dropins: [{ name: "20-nylzo-health.conf", contents: "[Unit]\nRequires=nylzo-ready.service\nAfter=nylzo-ready.service\n" }] },
    service("nylzo-update.service", "[Unit]\nRequires=nylzo-initialize.service\nAfter=nylzo-initialize.service network-online.target\n[Service]\nType=oneshot\nTimeoutStartSec=15min\nExecStart=/opt/nylzo/runtime/update check\n"),
    service("nylzo-update.timer", "[Unit]\nDescription=Check verified patch releases of Nylzo and Caddy\n[Timer]\nOnBootSec=5min\nOnUnitInactiveSec=10min\nRandomizedDelaySec=1min\nPersistent=true\n[Install]\nWantedBy=timers.target\n", site.automaticUpdates),
  ];
  const ignition = {
    ignition: { version: "3.4.0" },
    passwd: { groups: [{ name: "dockremap", gid: 2000 }, { name: "caddy", gid: 2001 }], users: [
      { name: "root", passwordHash: "*" }, { name: "core", passwordHash: "*", sshAuthorizedKeys: [site.administratorPublicKey] },
      { name: "dockremap", uid: 2000, primaryGroup: "dockremap", noCreateHome: true, shell: "/sbin/nologin" },
      { name: "caddy", uid: 2001, primaryGroup: "caddy", noCreateHome: true, shell: "/sbin/nologin" },
    ] }, storage: { files }, systemd: { units },
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
    ].map(([name, contents]) => ({ path: `/run/nylzo-image/${name}`, encoding: "gz+b64", content: gzipSync(contents!).toString("base64"), permissions: "0600" })),
    runcmd: [["/bin/bash", "-c", "set -a; . /run/nylzo-image/env; exec /bin/bash /run/nylzo-image/boot"]],
  });
  assert(Buffer.byteLength(cloud) <= 32768, "Image factory user_data exceeds 32 KiB");
  assert(Buffer.byteLength(JSON.stringify(ignition)) <= 32768, "Server user_data exceeds 32 KiB");
  return { site, ignition, installer, cloud };
}

if (import.meta.main) {
  const result = await render(JSON.parse(await readFile(process.argv[2]!, "utf8")));
  const destination = path.resolve(process.argv[3] ?? ".deploy/rendered");
  await mkdir(destination, { recursive: true, mode: 0o700 });
  for (const [name, contents] of Object.entries({ "server.ign": JSON.stringify(result.ignition), "installer.ign": JSON.stringify(result.installer), "image.cloud-config": result.cloud })) await writeFile(path.join(destination, name), contents, { mode: 0o600 });
  console.log(`Rendered image factory and server configuration in ${destination}`);
}
