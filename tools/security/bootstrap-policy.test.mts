import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { appendFileSync, cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";

test("privileged workflows pin the policy and reject tampered policy files before copying them", () => {
  const script = readFileSync("tools/security/bootstrap-policy", "utf8");
  const digest = createHash("sha256").update(script).digest("hex");
  for (const filename of [".github/workflows/security-updates.yml", ".github/workflows/release.yml"]) {
    const workflow = readFileSync(filename, "utf8");
    const pins = [...workflow.matchAll(/echo '([0-9a-f]{64})  tools\/security\/bootstrap-policy'/gu)];
    assert(pins.length > 0);
    for (const pin of pins) assert.equal(pin[1], digest);
  }
  const root = mkdtempSync(join(tmpdir(), "nylzo-policy-bootstrap-"));
  try {
    const files = [...script.matchAll(/^[0-9a-f]{64}  ([^\n]+)$/gmu)].map(match => match[1]!);
    for (const filename of [...files, "tools/security/bootstrap-policy"]) {
      mkdirSync(dirname(join(root, filename)), { recursive: true });
      cpSync(filename, join(root, filename));
    }
    const temporary = join(root, "temporary"); mkdirSync(temporary);
    const run = () => spawnSync("bash", ["tools/security/bootstrap-policy"], { cwd: root, encoding: "utf8", env: { ...process.env, RUNNER_TEMP: temporary } });
    assert.equal(run().status, 0);
    const copied = join(temporary, "nylzo-security-policy");
    assert(existsSync(join(copied, "mise.toml")));
    assert(existsSync(join(copied, "tools/security/automation.mts")));
    assert(existsSync(join(copied, "tools/dependencies/batches.cjs")));
    assert(existsSync(join(copied, "tools/dependencies/batch-state.mts")));
    rmSync(copied, { recursive: true });
    appendFileSync(join(root, "tools/security/dependency-policy.mts"), "\nthrow new Error('tampered');\n");
    assert.notEqual(run().status, 0);
    assert.equal(existsSync(copied), false);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
