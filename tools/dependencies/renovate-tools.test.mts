import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { externalArtifacts } from "./external.mts";
import { selectMise } from "./renovate-tools.mts";

test("native and custom lock generation use the newest reviewed main or published mise pin, above the safe-mode floor", () => {
  const template = externalArtifacts(path => readFileSync(path, "utf8")).find(item => item.name === "external-mise-amd64")!;
  const main = { ...template, version: "v2026.10.9" };
  const published = { ...main, version: "v2026.10.19", hashes: ["a".repeat(64)] };
  assert.equal(selectMise([main, published]), published);
  assert.equal(selectMise([published, main]), published);
  assert.equal(selectMise([main, { ...main }]), main);
  assert.throws(() => selectMise([{ ...main, version: "v2026.7.11" }]));
  assert.throws(() => selectMise([{ ...main, version: "v2026.10.19-preview" }]));
  assert.throws(() => selectMise([{ ...main, name: "external-attacker-amd64" }]));
});
