import { createHash } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";
import { gzipSync, gunzipSync } from "node:zlib";
import { render } from "./render.mts";

const site = JSON.parse(await readFile(process.argv[2]!, "utf8"));
const { ignition } = await render(site);
const decode = (file: any) => gunzipSync(Buffer.from(file.contents.source.split(",")[1], "base64")).toString();
const replace = (filename: string, contents: string) => {
  const file = ignition.storage.files.find(file => file.path === filename)!;
  file.contents.source = "data:;base64," + gzipSync(contents).toString("base64");
};
replace("/etc/caddy/Caddyfile", "{\nadmin off\nlog default {\nexclude http.log http.handlers http.stdlib\n}\n}\nhttp://:80 {\nheader -Server\nheader -Via\nreverse_proxy 172.30.81.2:8080 {\ntransport http {\nkeepalive 25s\n}\n}\n}\n");
const environment = ignition.storage.files.find(file => file.path === "/etc/nylzo/service.env")!;
replace(environment.path, decode(environment).replace(/^NYLZO_PUBLIC_URL=.*$/mu, "NYLZO_PUBLIC_URL=http://127.0.0.1"));
const server = process.argv[4]!;
// Runners share addresses, so they read these GitHub API documents with their own token and serve them.
// The updater still verifies the release, signatures and provenance they describe.
const updater = ignition.storage.files.find(file => file.path === "/opt/nylzo/runtime/update")!;
let script = decode(updater);
for (const read of [
  '    download "https://api.github.com/repos/misaon/nylzo/releases/tags/$release" "$temporary/release.json"\n',
  '    download "https://api.github.com/repos/misaon/nylzo/attestations/sha256:$digest" "$temporary/caddy-provenance.json"\n',
  '    download "https://api.github.com/repos/misaon/nylzo/releases/tags/$version" "$temporary/release.json"\n',
  '  download "https://api.github.com/repos/misaon/nylzo/attestations/$digest" "$temporary/provenance.json"\n',
]) {
  if (script.split(read).length !== 2) throw new Error("The updater's GitHub API reads changed; review the VM fixture");
  script = script.replace(read, read.replace('download "https://api.github.com/', `served "${server}/api/`));
}
const latest = '  download "https://api.github.com/repos/$1/releases/latest" "$temporary/release.json" || return\n';
if (script.split("api.github.com").length !== 2 || !script.includes(latest)) throw new Error("The updater reads another GitHub API resource; review the VM fixture");
script = script.replace("latest() {\n", 'served() {\n  curl --fail --silent --show-error --retry 5 --retry-connrefused --max-time 60 "$1" --output "$2"\n}\nlatest() {\n');
replace(updater.path, script);
const build = process.argv[5];
if (build !== undefined) {
  // Until the initial release ships a Caddy build, the VM starts on an older official Caddy (this revision's build
  // stands in for it) and installs the official release that host-tools.json approves through the verified path.
  const approved = JSON.parse(await readFile(new URL("../../deploy/hetzner-flatcar/toolchain.json", import.meta.url), "utf8")).caddy;
  const caddy = "/opt/nylzo/caddy/v0.0.0/caddy";
  const digest = createHash("sha256").update(await readFile(build)).digest("hex");
  ignition.storage.files.push({ path: "/var/lib/nylzo-deploy/caddy.tag", mode: 0o600, overwrite: true, contents: { source: "data:,v0.0.0%0A" } });
  ignition.storage.files.push({ path: "/var/lib/nylzo-deploy/caddy.approved.tag", mode: 0o600, overwrite: true, contents: { source: `data:,v${approved}%0A` } });
  ignition.storage.links = [...(ignition.storage.links ?? []), { path: "/opt/nylzo/bin/caddy", target: caddy, overwrite: true }];
  ignition.systemd.units.push({ name: "nylzo-vm-caddy.service", enabled: false, contents: `[Unit]\nWants=network-online.target\nAfter=network-online.target\nConditionPathExists=!${caddy}\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/usr/bin/mkdir -p /opt/nylzo/caddy/v0.0.0\nExecStart=/usr/bin/curl --fail --silent --show-error --retry 10 --retry-connrefused --max-time 300 --output ${caddy}.download ${server}/caddy\nExecStart=/usr/bin/bash -c "echo '${digest}  ${caddy}.download' | sha256sum --check --strict"\nExecStart=/usr/bin/chmod 0755 ${caddy}.download\nExecStart=/usr/bin/mv -T ${caddy}.download ${caddy}\n` });
}
ignition.storage.files.push({ path: "/opt/nylzo-vm-test", mode: 0o700, overwrite: true, contents: { compression: "gzip", source: "data:;base64," + gzipSync(await readFile(new URL("vm-acceptance", import.meta.url))).toString("base64") } });
ignition.systemd.units.push({ name: "nylzo-vm-test.service", enabled: true, contents: "[Unit]\nRequires=nylzo-ready.service\nAfter=nylzo-ready.service\n[Service]\nType=oneshot\nExecStart=/opt/nylzo-vm-test\nStandardOutput=journal+console\nStandardError=journal+console\nTimeoutStartSec=10min\n[Install]\nWantedBy=multi-user.target\n" });
ignition.systemd.units.push({ name: "nylzo-vm-debug.service", enabled: false, contents: "[Service]\nType=oneshot\nExecStart=/usr/bin/journalctl --no-pager -b -n 150 -u nylzo* -u caddy -u docker\nExecStart=/usr/bin/systemctl list-jobs --no-pager\n" });
ignition.systemd.units.push({ name: "nylzo-vm-debug.timer", enabled: true, contents: "[Timer]\nOnBootSec=2min\nOnUnitActiveSec=2min\nUnit=nylzo-vm-debug.service\n[Install]\nWantedBy=timers.target\n" });
ignition.systemd.units.push({ name: "nylzo-vm-failure.service", enabled: false, contents: "[Service]\nType=oneshot\nExecStart=/usr/bin/journalctl --no-pager -b -n 150 -u nylzo* -u caddy -u docker\nExecStart=/usr/bin/systemctl poweroff\n" });
for (const unit of ignition.systemd.units) {
  if (["nylzo-tools.service", "nylzo-bandwidth.service", "nylzo-initialize.service", "nylzo.service", "nylzo-vm-test.service", "nylzo-vm-caddy.service"].includes(unit.name) && "contents" in unit) {
    // A failed provisioning assertion must return its logs instead of idling
    // until the twenty-minute boot timeout. This drop-in exists only in CI.
    unit.contents += "\n[Unit]\nOnFailure=\nOnFailure=nylzo-vm-failure.service\n";
    if (unit.name === "nylzo-initialize.service" && build !== undefined) unit.contents += "Requires=nylzo-vm-caddy.service\nAfter=nylzo-vm-caddy.service\n";
  }
}
for (const unit of ignition.systemd.units) {
  // Keep the production journal output for sandboxed services. Giving the
  // capability-free Docker client a serial TTY would fail before ExecStart.
  if (["caddy.service", "nylzo.service"].includes(unit.name)) continue;
  if ("contents" in unit && typeof unit.contents === "string") unit.contents = unit.contents.replace(/^Standard(?:Output|Error)=.*\n/gmu, "").replace("[Service]\n", "[Service]\nStandardOutput=tty\nStandardError=tty\nTTYPath=/dev/ttyS0\n");
}
await writeFile(process.argv[3]!, JSON.stringify(ignition));
