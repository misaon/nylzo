import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { externalArtifacts, externalInputs, prepareExternal, verifyExternalBytes, type ExternalArtifact } from "./external.mts";

test("every locked provider and host asset has both platform inventories; inconsistent pins fail closed", () => {
  const files = new Map(externalInputs.map(path => [path, readFileSync(path, "utf8")]));
  const read = (path: string) => files.get(path)!;
  const artifacts = externalArtifacts(read);
  assert.equal(artifacts.length, 18);
  for (const name of ["provider-hcloud", "provider-cloudflare", "gh", "cosign", "jq", "ignition", "syft", "mise", "codecov"]) {
    for (const architecture of ["amd64", "arm64"]) assert(artifacts.some(item => item.name === `external-${name}-${architecture}`));
  }
  assert(artifacts.find(item => item.name === "external-provider-hcloud-arm64")!.dependencies.includes("hetznercloud/hcloud"));
  assert(artifacts.find(item => item.name === "external-ignition-arm64")!.url.endsWith("ignition-validate-aarch64-linux"));
  const path = externalInputs[1]!, original = read(path);
  files.set(path, original.replaceAll("hetznercloud/hcloud", "attacker/hcloud"));
  assert.throws(() => externalArtifacts(read));
  files.set(path, original.replaceAll(/"zh:[a-f0-9]+"/gu, '"h1:unusable"'));
  assert.throws(() => externalArtifacts(read));
  files.set(path, original.replace('version     = "1.70.2"', 'version     = "1.70.3"'));
  assert.throws(() => externalArtifacts(read));
});

test("external downloads are verified before parsing and only the selected archive member is read", async () => {
  const root = mkdtempSync(join(tmpdir(), "nylzo-external-test-")), originalFetch = globalThis.fetch;
  try {
    const input = join(root, "input"); mkdirSync(input);
    writeFileSync(join(input, "expected"), "#!/bin/sh\ntouch SHOULD_NOT_EXECUTE\n");
    writeFileSync(join(input, "unrelated"), "other archive member");
    const archive = join(root, "archive.tar.gz"); execFileSync("tar", ["-czf", archive, "-C", input, "expected", "unrelated"]);
    const bytes = readFileSync(archive), digest = createHash("sha256").update(bytes).digest("hex");
    const artifact: ExternalArtifact = { name: "external-test-amd64", dependencies: ["example/test"], version: "1.0.0", url: "https://github.com/example/test/releases/download/v1.0.0/test.tar.gz", hashes: [digest], format: "tar", member: "expected" };
    globalThis.fetch = async () => new Response(bytes);
    const output = join(root, "verified"); const target = await prepareExternal(artifact, output);
    assert.equal(readFileSync(target, "utf8"), readFileSync(join(input, "expected"), "utf8"));
    assert(!existsSync(join(output, "unrelated")) && !existsSync(join(output, "SHOULD_NOT_EXECUTE")));
    assert.throws(() => verifyExternalBytes({ ...artifact, hashes: ["a".repeat(64)] }, bytes));
    const rejected = join(root, "rejected");
    await assert.rejects(prepareExternal({ ...artifact, hashes: ["a".repeat(64)] }, rejected));
    assert(!existsSync(join(rejected, "download")) && !existsSync(join(rejected, "expected")));
    await assert.rejects(prepareExternal({ ...artifact, member: "missing" }, join(root, "missing")));
  } finally { globalThis.fetch = originalFetch; rmSync(root, { recursive: true, force: true }); }
});
