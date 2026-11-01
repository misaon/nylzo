import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";
import { archive, hostSources, manifestEntries } from "./bundle.mts";

const sources = await hostSources();
const image = /"(ubuntu:\d+\.\d+@sha256:[a-f0-9]{64})"/u.exec(readFileSync(resolve("tools/deploy/runtime.test.mts"), "utf8"))![1]!;
const available = spawnSync("docker", ["info"], { stdio: "ignore" }).status === 0;

test("the host bundle is reproducible and every file has one place and one role on the host", () => {
  assert.deepEqual(archive(sources), archive(new Map([...sources].reverse())));
  const pattern = new RegExp(/^target_pattern='(.+)'$/mu.exec(sources.get("update/activate")!)![1]!, "u");
  const entries = manifestEntries(sources);
  const targets = entries.map(entry => entry.target);
  assert.equal(new Set(targets).size, targets.length, "Two manifest entries install the same target");
  for (const { target, source } of entries) {
    assert.match(target, pattern, `activate refuses ${target}`);
    assert(sources.has(source), `missing ${source}`);
  }
  const composed = ["proxy/Caddyfile", "proxy/www.Caddyfile", "proxy/monitor.Caddyfile"];
  for (const name of sources.keys()) assert(["manifest", "units", ...composed].includes(name) || entries.some(entry => entry.source === name), `${name} is never installed`);
  for (const line of sources.get("units")!.trimEnd().split("\n")) {
    const [action, , unit] = line.split(" ");
    if (action === "enable") assert(targets.includes(`/etc/systemd/system/${unit}`), `${unit} is enabled but not installed`);
  }
  const sandbox = /^ReadWritePaths=(.+)$/mu.exec(sources.get("update/sandbox.conf")!)![1]!.split(" ");
  for (const target of targets.filter(target => target.startsWith("/etc/"))) {
    assert(sandbox.some(directory => target.startsWith(`${directory}/`)), `the sandboxed updater cannot write ${target}`);
  }
  for (const name of [...sources.keys()].filter(name => /\.(service|timer)$/u.test(name))) {
    assert.match(sources.get(name)!, /^\[(Unit|Service|Timer)\]$/mu, name);
  }
  for (const name of ["proxy/caddy.service", "application/nylzo.service", "monitoring/nylzo-clock.service", "monitoring/nylzo-heartbeat.service"]) {
    const unit = sources.get(name)!;
    for (const setting of ["NoNewPrivileges=true", "ProtectSystem=strict", "RestrictNamespaces=true", "SystemCallFilter=@system-service", "LimitCORE=0"]) assert(unit.includes(`${setting}\n`), `${name} lacks ${setting}`);
  }
});

test("hosts install a bundle atomically, apply only what changed, and roll back to the previous generation", { skip: !available, timeout: 120000 }, () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-bundle-"));
  try {
    writeFileSync(join(directory, "initial.tar"), archive(sources));
    const next = new Map(sources);
    next.set("system/sshd.conf", sources.get("system/sshd.conf")! + "ClientAliveInterval 60\n");
    next.set("monitoring/nylzo-clock.timer", sources.get("monitoring/nylzo-clock.timer")!.replace("OnUnitActiveSec=15min", "OnUnitActiveSec=10min"));
    next.set("manifest", sources.get("manifest")!.split("\n").filter(line => !line.includes("heartbeat")).join("\n"));
    next.set("units", sources.get("units")!.split("\n").filter(line => !line.includes("heartbeat")).join("\n"));
    next.delete("monitoring/heartbeat");
    next.delete("monitoring/nylzo-heartbeat.service");
    next.delete("monitoring/nylzo-heartbeat.timer");
    writeFileSync(join(directory, "next.tar"), archive(next));
    writeFileSync(join(directory, "foreign.tar"), archive(new Map([...sources, ["manifest", sources.get("manifest")! + "/etc/passwd system/sshd.conf\n"]])));
    writeFileSync(join(directory, "format.tar"), archive(new Map([...sources, ["manifest", sources.get("manifest")!.replace("format 1", "format 2")]])));
    const result = spawnSync("docker", ["run", "--interactive", "--rm", "--network", "none", "--cap-drop", "ALL", "--cap-add", "CHOWN", "--cap-add", "DAC_OVERRIDE", "--cap-add", "FOWNER",
      "--mount", `type=bind,source=${directory},target=/bundles,readonly`, image, "bash", "-s"], { encoding: "utf8", timeout: 100000, input: String.raw`set -euo pipefail
trap 'echo "failed at line $LINENO: $BASH_COMMAND" >&2' ERR
mkdir -p /fake /etc/nylzo /opt/nylzo/host /opt/nylzo/bin
printf '#!/bin/bash\nprintf "%%s\\n" "$*" >> /tmp/systemctl\n' > /fake/systemctl
printf '#!/bin/bash\nprintf "%%s\\n" "$*" >> /tmp/systemd-run\n' > /fake/systemd-run
printf '#!/bin/bash\nexit 0\n' > /opt/nylzo/bin/caddy
chmod 0755 /fake/* /opt/nylzo/bin/caddy
export PATH="/fake:$PATH"
printf 'AUTOMATIC_UPDATES=true\nPUBLIC_ACCESS=false\nWWW_REDIRECT=true\nADMINISTRATOR_NETWORKS=192.0.2.1/32\n' > /etc/nylzo/host.env
printf 'NYLZO_DOMAIN=nylzo.example.org\n' > /etc/nylzo/caddy.env
printf 'certificate\n' > /etc/nylzo/monitor-client.pem
stage() {
  mkdir -p "/opt/nylzo/host/$2"
  tar --extract --file "/bundles/$1" --directory "/opt/nylzo/host/$2" --no-same-owner --no-same-permissions
}
stage initial.tar initial
bash /opt/nylzo/host/initial/update/activate --first-boot /opt/nylzo/host/initial
[[ "$(readlink /opt/nylzo/host/current)" = /opt/nylzo/host/initial && ! -e /opt/nylzo/host/previous ]]
[[ "$(stat -c %a /opt/nylzo/runtime/update)" = 755 && "$(stat -c %a /etc/systemd/system/nylzo.service)" = 644 ]]
grep -q '^KexAlgorithms mlkem768x25519-sha256,sntrup761x25519-sha512$' /etc/ssh/sshd_config.d/20-nylzo.conf
grep -q '^www\.{\$NYLZO_DOMAIN} {$' /etc/caddy/Caddyfile
grep -q '^https://{\$NYLZO_DOMAIN}:8443 {$' /etc/caddy/Caddyfile
[[ -s /etc/systemd/system/nylzo-update.service.d/20-sandbox.conf && -s /etc/systemd/system/nylzo-initialize.service.d/20-sandbox.conf ]]
grep -qx 'enable --quiet nylzo-update.timer' /tmp/systemctl
grep -qx 'disable --quiet --now nylzo-heartbeat.timer' /tmp/systemctl
grep -qx 'mask --quiet getty@tty1.service' /tmp/systemctl
grep -qx 'restart nylzo-firewall.service' /tmp/systemctl
grep -q '^start --no-block nylzo-firewall.service .*nylzo-initialize.service' /tmp/systemctl
if grep -q try-restart /tmp/systemctl || [[ -e /tmp/systemd-run ]]; then exit 1; fi
: > /tmp/systemctl
printf 'AUTOMATIC_UPDATES=false\nPUBLIC_ACCESS=false\nWWW_REDIRECT=false\nADMINISTRATOR_NETWORKS=192.0.2.1/32\n' > /etc/nylzo/host.env
stage next.tar release-v0.3.0
bash /opt/nylzo/host/release-v0.3.0/update/activate /opt/nylzo/host/release-v0.3.0
[[ "$(readlink /opt/nylzo/host/current)" = /opt/nylzo/host/release-v0.3.0 && "$(readlink /opt/nylzo/host/previous)" = /opt/nylzo/host/initial ]]
[[ ! -e /opt/nylzo/runtime/heartbeat && ! -e /etc/systemd/system/nylzo-heartbeat.timer ]]
grep -qx 'ClientAliveInterval 60' /etc/ssh/sshd_config.d/20-nylzo.conf
if grep -q '^www\.' /etc/caddy/Caddyfile; then exit 1; fi
grep -q 'caddy validate --config /run/nylzo-caddyfile\.' /tmp/systemd-run
grep -qx 'disable --quiet --now nylzo-update.timer' /tmp/systemctl
grep -qx 'disable --quiet --now nylzo-heartbeat.timer' /tmp/systemctl
grep -qx 'try-restart caddy.service nylzo-clock.timer' /tmp/systemctl
for refused in foreign.tar format.tar; do
  stage "$refused" release-v9.9.9
  if bash /opt/nylzo/host/release-v9.9.9/update/activate /opt/nylzo/host/release-v9.9.9 2>/dev/null; then exit 1; fi
  rm -rf /opt/nylzo/host/release-v9.9.9
done
[[ "$(readlink /opt/nylzo/host/current)" = /opt/nylzo/host/release-v0.3.0 && -s /etc/passwd ]] && ! grep -q PermitRootLogin /etc/passwd
stage next.tar release-v9.9.8
ln -s /etc/shadow /opt/nylzo/host/release-v9.9.8/system/link
if bash /opt/nylzo/host/release-v9.9.8/update/activate /opt/nylzo/host/release-v9.9.8 2>/dev/null; then exit 1; fi
if bash /opt/nylzo/host/initial/update/activate /tmp 2>/dev/null; then exit 1; fi
bash /opt/nylzo/host/initial/update/activate /opt/nylzo/host/initial
[[ "$(readlink /opt/nylzo/host/current)" = /opt/nylzo/host/initial && "$(readlink /opt/nylzo/host/previous)" = /opt/nylzo/host/release-v0.3.0 ]]
[[ -x /opt/nylzo/runtime/heartbeat && -s /etc/systemd/system/nylzo-heartbeat.timer ]]
if grep -q ClientAliveInterval /etc/ssh/sshd_config.d/20-nylzo.conf; then exit 1; fi
echo HOST_BUNDLE_VERIFIED
` });
    assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
    assert.match(result.stdout, /HOST_BUNDLE_VERIFIED/u);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
