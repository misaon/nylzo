import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const [name, architecture] = process.argv.slice(2);
assert(name === "ignition" && ["amd64", "arm64"].includes(architecture!));
const entry = JSON.parse(readFileSync("tools/dependencies/assets.json", "utf8"))[`${name}-${architecture}`];
assert.equal(entry.package, "coreos/ignition");
assert.match(entry.version, /^v\d+\.\d+\.\d+$/u);
assert.match(entry.sha256, /^[a-f0-9]{64}$/u);
console.log(entry.version);
console.log(entry.sha256);
