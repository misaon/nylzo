import { mkdir, readFile, writeFile } from "node:fs/promises";
import { gzipSync, gunzipSync } from "node:zlib";
import { render } from "./render.mts";

const { installer } = await render(JSON.parse(await readFile(process.argv[2]!, "utf8")));
const script = installer.storage.files.find(file => file.path === "/opt/nylzo-image/install")!;
const contents = gunzipSync(Buffer.from(script.contents.source.split(",")[1]!, "base64")).toString();
// Only the cloud metadata identity check is substituted in this isolated VM.
// Signature verification, disk selection, writing and shutdown use the real installer.
const metadata = '[[ "$(curl --fail --silent --max-time 3 http://169.254.169.254/hetzner/v1/metadata/hostname)" = "$EXPECTED_BUILDER_NAME" ]]';
if (!contents.includes(metadata)) throw new Error("Installer identity check changed; review the VM fixture");
script.contents.source = "data:;base64," + gzipSync(contents.replace(metadata, '[[ "$EXPECTED_BUILDER_NAME" = "nylzo-test-image-builder" ]]')).toString("base64");
const unit = installer.systemd.units[0]!;
unit.contents = unit.contents.replace("[Service]\n", "[Service]\nStandardOutput=journal+console\nStandardError=journal+console\n");
installer.systemd.units.push({ name: "nylzo-image-debug.service", enabled: true, contents: "[Unit]\nAfter=nylzo-image-install.service\n[Service]\nType=oneshot\nExecStart=/usr/bin/journalctl --no-pager -b -u nylzo-image-install.service\nStandardOutput=tty\nStandardError=tty\nTTYPath=/dev/ttyS0\n[Install]\nWantedBy=multi-user.target\n" } as any);
const destination = `${process.argv[3]}/usr/share/oem`;
await mkdir(destination, { recursive: true });
await writeFile(`${destination}/config.ign`, JSON.stringify(installer));
