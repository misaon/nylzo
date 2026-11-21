import { mkdir, readFile, writeFile } from "node:fs/promises";
import { gzipSync, gunzipSync } from "node:zlib";
import { hostSources } from "./bundle.mts";
import { render } from "./render.mts";

const [siteFile, image] = process.argv.slice(2);
if (!siteFile || !image) throw new Error("Usage: bun tools/deploy/image-vm-fixture.mts SITE_JSON IMAGE_DIRECTORY");
const { installer } = await render(JSON.parse(await readFile(siteFile, "utf8")), await hostSources());
const script = installer.storage.files.find(file => file.path === "/opt/nylzo-image/install");
if (script === undefined) throw new Error("The installer has no install script; review the VM fixture");
const contents = gunzipSync(Buffer.from(script.contents.source.slice(script.contents.source.indexOf(",") + 1), "base64")).toString();
// Only the cloud metadata identity check is substituted in this isolated VM.
// Signature verification, disk selection, writing and shutdown use the real installer.
const metadata = '[[ "$(curl --fail --silent --max-time 3 http://169.254.169.254/hetzner/v1/metadata/hostname)" = "$EXPECTED_BUILDER_NAME" ]]';
if (!contents.includes(metadata)) throw new Error("Installer identity check changed; review the VM fixture");
script.contents.source = "data:;base64," + gzipSync(contents.replace(metadata, '[[ "$EXPECTED_BUILDER_NAME" = "nylzo-test-image-builder" ]]')).toString("base64");
const [unit] = installer.systemd.units;
if (unit === undefined || !("contents" in unit)) throw new Error("The installer unit has no contents; review the VM fixture");
unit.contents = unit.contents.replace("[Service]\n", "[Service]\nStandardOutput=journal+console\nStandardError=journal+console\n");
installer.systemd.units.push({ name: "nylzo-image-debug.service", enabled: true, contents: "[Unit]\nAfter=nylzo-image-install.service\n[Service]\nType=oneshot\nExecStart=/usr/bin/journalctl --no-pager -b -u nylzo-image-install.service\nStandardOutput=tty\nStandardError=tty\nTTYPath=/dev/ttyS0\n[Install]\nWantedBy=multi-user.target\n" });
const destination = `${image}/usr/share/oem`;
await mkdir(destination, { recursive: true });
await writeFile(`${destination}/config.ign`, JSON.stringify(installer));
