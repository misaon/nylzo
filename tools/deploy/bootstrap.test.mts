import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

test("both image stages reject a different or additional primary signing key", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-signing-key-"));
  try {
    writeFileSync(join(directory, "gpg"), '#!/bin/sh\nprintf "%s\\n" "$KEY_LIST"\n');
    chmodSync(join(directory, "gpg"), 0o700);
    const key = (fingerprint: string) => `pub:::::::::\nfpr:::::::::${fingerprint}:\n`;
    const expected = "F88CFEDEFF29A5B4D9523864E25D9AED0593B34A";
    for (const stage of ["boot", "install"]) {
      const source = readFileSync(`deploy/hetzner-flatcar/bootstrap/${stage}`, "utf8");
      const guard = source.split("\n").filter(line => line.startsWith("fingerprint=") || line.startsWith('[[ "$fingerprint"')).join("\n");
      assert.equal(guard.split("\n").length, 2);
      const run = (keys: string) => spawnSync("bash", ["-euo", "pipefail", "-c", guard], { encoding: "utf8", env: { ...process.env, PATH: `${directory}:${process.env.PATH}`, KEY_LIST: keys } });
      assert.equal(run(key(expected)).status, 0, stage);
      for (const keys of [key("A".repeat(40)), key(expected) + key("A".repeat(40)), `sub:::::::::\nfpr:::::::::${expected}:\n`, ""]) {
        assert.notEqual(run(keys).status, 0, stage);
      }
    }
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

test("SELinux acceptance rejects a successful probe, timeout, or unrelated error", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-selinux-assertion-"));
  try {
    const source = readFileSync("tools/deploy/vm-acceptance", "utf8");
    const assertion = source.match(/^if \[\[ "\$status" != 1 \]\][\s\S]*?^fi$/mu)?.[0];
    assert(assertion);
    for (const [status, message, accepted] of [[1, "open /run/nylzo-pepper: permission denied", true], [0, "permission denied", false], [124, "permission denied", false], [125, "permission denied", false], [1, "an unrelated error", false]] as const) {
      writeFileSync(join(directory, "result"), message);
      const result = spawnSync("bash", ["-euo", "pipefail", "-c", assertion], { encoding: "utf8", env: { ...process.env, status: String(status), probe: directory } });
      assert.equal(result.status === 0, accepted, `${status}: ${message}`);
    }
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
