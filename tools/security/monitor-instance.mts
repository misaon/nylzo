import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

let directory: string | undefined;
let phase = "configuration";
try {
  const server = new URL(process.env.URL!);
  assert.equal(server.protocol, "https:");
  assert.equal(server.username + server.password + server.search + server.hash, "");
  assert.equal(server.pathname, "/");
  const cli = resolve(process.env.NYLZO_MONITOR_CLI ?? "./nylzo");
  directory = mkdtempSync(join(process.env.RUNNER_TEMP ?? tmpdir(), "nylzo-monitor-"));
  chmodSync(directory, 0o700);
  const link = join(directory, "link");
  const payload = Buffer.from(`Nylzo synthetic availability check ${randomBytes(32).toString("hex")}\n`);
  const run = (arguments_: string[], input?: Buffer) => {
    const result = spawnSync(cli, arguments_, { input, timeout: 60_000, maxBuffer: 1024 * 1024 });
    assert.ifError(result.error);
    return result;
  };
  phase = "message creation";
  const created = run(["create", "--server", server.origin, "--ttl", "5m", "--opens", "1"], payload);
  assert.equal(created.status, 0);
  writeFileSync(link, created.stdout, { mode: 0o600, flag: "wx" });
  phase = "message decryption";
  const opened = run(["read", "--link-file", link, "--stdout"]);
  assert.equal(opened.status, 0);
  assert.deepEqual(opened.stdout, payload);
  phase = "consumed-message rejection";
  const consumed = run(["read", "--link-file", link, "--stdout"]);
  assert.equal(consumed.status, 1);
  assert.match(consumed.stderr.toString("utf8"), /the message was already opened/u);
  console.log("The instance created, decrypted, and consumed a one-open synthetic message.");
} catch {
  // CLI errors may contain a capability URL. Publish only the failed phase.
  console.error(`Instance lifecycle check failed during ${phase}.`);
  process.exitCode = 1;
} finally {
  if (directory) rmSync(directory, { recursive: true, force: true });
}
