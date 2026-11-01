import { mkdir, writeFile } from "node:fs/promises";
import { releaseBundle, render } from "./render.mts";

const destination = process.argv[2] ?? ".deploy/validation";
await mkdir(destination, { recursive: true, mode: 0o700 });
for (const architecture of ["amd64", "arm64"]) {
  const result = await render({
    name: "nylzo-test", domain: "nylzo.example.org", location: "nbg1",
    serverType: architecture === "arm64" ? "cax11" : "cx23", architecture,
    administratorNetworks: ["192.0.2.1/32"],
    administratorPublicKey: "ssh-ed25519 " + Buffer.concat([Buffer.from("0000000b7373682d6564323535313900000020", "hex"), Buffer.alloc(32, 1)]).toString("base64"),
    reportPublicKey: Buffer.alloc(1216, 1).toString("base64url"),
    operator: "Test operator", contact: "security@example.org", hosting: "Test provider",
    dailyCreates: 250, initialRelease: "v0.1.2", automaticUpdates: true,
    heartbeatUrl: "https://heartbeat.example.org/ping/00000000-0000-4000-8000-000000000000",
    tang: { threshold: 2, servers: [{ url: "http://tang-one.example.org", thumbprint: "A".repeat(43) }, { url: "http://tang-two.example.net", thumbprint: "B".repeat(43) }] },
  }, releaseBundle("v0.1.2", "0".repeat(64)));
  for (const [name, document] of Object.entries({ server: result.ignition, installer: result.installer })) {
    await writeFile(`${destination}/${architecture}-${name}.ign`, JSON.stringify(document));
  }
  if (architecture === "amd64") {
    const { heartbeatUrl, tang, ...site } = result.site;
    await writeFile(`${destination}/site.json`, JSON.stringify(site));
  }
}
