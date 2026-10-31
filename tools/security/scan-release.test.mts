import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

test("release scanning covers every archive and image architecture and fails closed", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-grype-"));
  const image = `ghcr.io/misaon/nylzo@sha256:${"a".repeat(64)}`;
  const caddy = `ghcr.io/misaon/nylzo-caddy@sha256:${"b".repeat(64)}`;
  const archives = [
    "darwin_amd64.tar.gz",
    "darwin_arm64.tar.gz",
    "linux_amd64.tar.gz",
    "linux_arm64.tar.gz",
    "windows_amd64.zip",
    "windows_arm64.zip",
  ];
  try {
    for (const archive of archives) writeFileSync(join(directory, `nylzo_1.2.3_${archive}.sbom.json`), "{}\n");
    for (const architecture of ["amd64", "arm64"]) writeFileSync(join(directory, `nylzo-caddy_1.2.3_linux_${architecture}.tar.gz.sbom.json`), "{}\n");
    const executable = join(directory, "grype");
    const log = join(directory, "calls");
    writeFileSync(
      executable,
      '#!/bin/sh\nprintf "%s\\n" "$*" >> "$SCAN_LOG"\ntest "$GRYPE_DB_VALIDATE_AGE" = true || exit 9\ntest "$GRYPE_DB_MAX_ALLOWED_BUILT_AGE" = 48h || exit 9\nwhile [ "$#" -gt 0 ]; do\n  if [ "$1" = --file ]; then shift; printf \'{"matches":[],"descriptor":{"name":"grype"}}\' > "$1"; fi\n  shift\ndone\nexit "${SCAN_EXIT:-0}"\n',
    );
    chmodSync(executable, 0o700);
    const scan = (reference = image, exit = "0", caddyReference = caddy) =>
      spawnSync("bash", ["tools/security/scan-release", reference, caddyReference, directory, join(directory, "reports")], {
        encoding: "utf8",
        env: { ...process.env, PATH: `${directory}:${process.env.PATH}`, SCAN_LOG: log, SCAN_EXIT: exit },
      });
    assert.equal(scan().status, 0);
    const calls = readFileSync(log, "utf8").trim().split("\n");
    assert.equal(calls.length, 12);
    assert.equal(calls.filter((call) => call.startsWith("sbom:")).length, 8);
    for (const architecture of ["amd64", "arm64"])
      for (const reference of [image, caddy])
        assert.ok(calls.includes(`registry:${reference} --platform linux/${architecture} --fail-on high --output json --file ${join(directory, "reports", `${reference === caddy ? "caddy-" : ""}image-linux-${architecture}.grype.json`)}`));
    assert.ok(calls.every((call) => call.includes("--fail-on high")));
    assert.notEqual(scan(image, "2").status, 0);
    assert.equal(scan(image, "1").status, 2, "Operational failures cannot be mistaken for detected vulnerabilities, even with valid JSON reports");
    const invalid = scan("ghcr.io/other/project:latest");
    assert.notEqual(invalid.status, 0, invalid.stderr);
    assert.notEqual(scan("ghcr.io/misaon/nylzo:latest").status, 0);
    assert.notEqual(scan(image, "0", `caddy@sha256:${"b".repeat(64)}`).status, 0);
    assert.notEqual(scan(image, "0", image).status, 0);
    rmSync(join(directory, `nylzo_1.2.3_${archives[0]}.sbom.json`));
    assert.notEqual(scan().status, 0);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
