import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

type Asset = { package: string; version: string; sha256: string };

function isAsset(value: unknown): value is Asset {
  return typeof value === "object" && value !== null &&
    "package" in value && typeof value.package === "string" &&
    "version" in value && typeof value.version === "string" &&
    "sha256" in value && typeof value.sha256 === "string";
}

const [name, architecture] = process.argv.slice(2);
assert(name === "ignition" && architecture !== undefined && ["amd64", "arm64"].includes(architecture));
const assets: unknown = JSON.parse(readFileSync("tools/dependencies/assets.json", "utf8"));
const entry: unknown = typeof assets === "object" && assets !== null ? Reflect.get(assets, `${name}-${architecture}`) : undefined;
assert(isAsset(entry), `tools/dependencies/assets.json holds no ${name}-${architecture} asset`);
assert.equal(entry.package, "coreos/ignition");
assert.match(entry.version, /^v\d+\.\d+\.\d+$/u);
assert.match(entry.sha256, /^[a-f0-9]{64}$/u);
console.log(entry.version);
console.log(entry.sha256);
