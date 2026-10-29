import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { chmodSync, copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import test from "node:test";

test("raising the mise minimum preserves unchanged runtime locks; executable changes still require successful regeneration", () => {
  const root = mkdtempSync(join(tmpdir(), "nylzo-sync-test-"));
  try {
    const bootstrap = readFileSync("tools/security/bootstrap-policy", "utf8");
    const paths = ["deploy/compose/compose.yaml", "mise.toml", "tools/dependencies/assets.json", "deploy/hetzner-flatcar/toolchain.json", "mise-tasks/deploy", "tools/security/bootstrap-policy", ".github/workflows/security-updates.yml", ".github/workflows/release.yml", ...[...bootstrap.matchAll(/^[a-f0-9]{64}  ([^\n]+)$/gmu)].map(match => match[1]!)];
    for (const path of paths) { mkdirSync(dirname(join(root, path)), { recursive: true }); copyFileSync(path, join(root, path)); }
    mkdirSync(join(root, "bin"));
    writeFileSync(join(root, "bin/mise"), "#!/bin/sh\ntouch \"$SYNC_CALLS\"\nexit 87\n"); chmodSync(join(root, "bin/mise"), 0o700);
    const pins = readFileSync(join(root, "mise.toml"), "utf8").replace(/^min_version = "[^"]+"/mu, 'min_version = "2099.1.1"');
    writeFileSync(join(root, "mise.toml"), pins);
    const lock = readFileSync(join(root, "tools/security/runtime.lock"), "utf8");
    const run = () => spawnSync(process.execPath, [resolve("tools/dependencies/sync.mts"), "--locks"], { cwd: root, encoding: "utf8", env: { ...process.env, PATH: `${root}/bin:${process.env.PATH}`, SYNC_CALLS: join(root, "calls") } });
    const unchanged = run(); assert.equal(unchanged.status, 0, unchanged.stderr);
    assert(!existsSync(join(root, "calls")));
    assert.equal(readFileSync(join(root, "tools/security/runtime.lock"), "utf8"), lock);
    assert(readFileSync(join(root, "tools/security/runtime.toml"), "utf8").includes('min_version = "2099.1.1"'));
    for (const match of readFileSync(join(root, "tools/security/bootstrap-policy"), "utf8").matchAll(/^([a-f0-9]{64})  ([^\n]+)$/gmu)) assert.equal(match[1], createHash("sha256").update(readFileSync(join(root, match[2]!))).digest("hex"));
    writeFileSync(join(root, "mise.toml"), pins.replace(/^node = "[^"]+"/mu, 'node = "99.0.0"'));
    assert.notEqual(run().status, 0);
    assert(existsSync(join(root, "calls")));
  } finally { rmSync(root, { recursive: true, force: true }); }
});
