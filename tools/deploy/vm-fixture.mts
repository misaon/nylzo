import { readFile, writeFile } from "node:fs/promises";
import { gzipSync, gunzipSync } from "node:zlib";
import { render } from "./render.mts";

const { ignition } = await render(JSON.parse(await readFile(process.argv[2]!, "utf8")));
const decode = (file: any) => gunzipSync(Buffer.from(file.contents.source.split(",")[1], "base64")).toString();
const replace = (filename: string, contents: string) => {
  const file = ignition.storage.files.find(file => file.path === filename)!;
  file.contents.source = "data:;base64," + gzipSync(contents).toString("base64");
};
replace("/etc/caddy/Caddyfile", "{\nadmin off\nlog default {\nexclude http.log http.handlers http.stdlib\n}\n}\nhttp://:80 {\nheader -Server\nheader -Via\nreverse_proxy 172.30.81.2:8080 {\ntransport http {\nkeepalive 25s\n}\n}\n}\n");
const environment = ignition.storage.files.find(file => file.path === "/etc/nylzo/service.env")!;
replace(environment.path, decode(environment).replace(/^NYLZO_PUBLIC_URL=.*$/mu, "NYLZO_PUBLIC_URL=http://127.0.0.1"));
ignition.storage.files.push({ path: "/opt/nylzo-vm-test", mode: 0o700, overwrite: true, contents: { compression: "gzip", source: "data:;base64," + gzipSync(await readFile(new URL("vm-acceptance", import.meta.url))).toString("base64") } });
ignition.systemd.units.push({ name: "nylzo-vm-test.service", enabled: true, contents: "[Unit]\nRequires=nylzo-ready.service\nAfter=nylzo-ready.service\n[Service]\nType=oneshot\nExecStart=/opt/nylzo-vm-test\nStandardOutput=journal+console\nStandardError=journal+console\nTimeoutStartSec=10min\n[Install]\nWantedBy=multi-user.target\n" });
ignition.systemd.units.push({ name: "nylzo-vm-debug.service", enabled: false, contents: "[Service]\nType=oneshot\nExecStart=/usr/bin/journalctl --no-pager -b -n 150 -u nylzo* -u caddy -u docker\nExecStart=/usr/bin/systemctl list-jobs --no-pager\n" });
ignition.systemd.units.push({ name: "nylzo-vm-debug.timer", enabled: true, contents: "[Timer]\nOnBootSec=2min\nOnUnitActiveSec=2min\nUnit=nylzo-vm-debug.service\n[Install]\nWantedBy=timers.target\n" });
ignition.systemd.units.push({ name: "nylzo-vm-failure.service", enabled: false, contents: "[Service]\nType=oneshot\nExecStart=/usr/bin/journalctl --no-pager -b -n 150 -u nylzo* -u caddy -u docker\nExecStart=/usr/bin/systemctl poweroff\n" });
for (const unit of ignition.systemd.units) {
  if (["nylzo-tools.service", "nylzo-bandwidth.service", "nylzo-initialize.service", "nylzo-ready.service", "nylzo-vm-test.service"].includes(unit.name) && "contents" in unit) {
    // A failed provisioning assertion must return its logs instead of idling
    // until the twenty-minute boot timeout. This drop-in exists only in CI.
    unit.contents += "\n[Unit]\nOnFailure=\nOnFailure=nylzo-vm-failure.service\n";
  }
}
for (const unit of ignition.systemd.units) {
  if (unit.name === "caddy.service") continue;
  if ("contents" in unit && typeof unit.contents === "string") unit.contents = unit.contents.replace(/^Standard(?:Output|Error)=.*\n/gmu, "").replace("[Service]\n", "[Service]\nStandardOutput=tty\nStandardError=tty\nTTYPath=/dev/ttyS0\n");
}
await writeFile(process.argv[3]!, JSON.stringify(ignition));
