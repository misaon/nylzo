import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

const available = ["jq", "sha256sum"].every(command => spawnSync("bash", ["-c", `command -v ${command}`], { stdio: "ignore" }).status === 0);
test("a minor or major upgrade needs valid signed evidence, the exact asset hash and planned-release metadata", { skip: !available }, () => {
  const root = mkdtempSync(join(tmpdir(), "nylzo-release-policy-"));
  try {
    mkdirSync(join(root, "bin"));
    // macOS may provide a reduced sha256sum; production uses GNU coreutils.
    const gnu = spawnSync("bash", ["-c", "command -v gsha256sum"], { encoding: "utf8" });
    if (gnu.status === 0) writeFileSync(join(root, "bin/sha256sum"), `#!/bin/sh\nexec "${gnu.stdout.trim()}" "$@"\n`);
    writeFileSync(join(root, "bin/curl"), '#!/usr/bin/env bash\nset -eu\nfile="${@: -1}"\ncp "$FIXTURE/$file" "$file"\nif [[ "${CORRUPT:-}" = true && "$file" = *.release-policy.json ]]; then printf "changed" >> "$file"; fi\n');
    writeFileSync(join(root, "bin/cosign"), '#!/usr/bin/env bash\nexit "${SIGNATURE_STATUS:-0}"\n');
    for (const name of ["curl", "cosign", ...(gnu.status === 0 ? ["sha256sum"] : [])]) chmodSync(join(root, "bin", name), 0o700);
    const run = (metadata: object, overrides: Record<string,string> = {}) => {
      const bytes = JSON.stringify(metadata), name = "nylzo_1.0.0.release-policy.json";
      writeFileSync(join(root, name), bytes);
      writeFileSync(join(root, "nylzo_1.0.0.evidence.sha256"), `${createHash("sha256").update(bytes).digest("hex")}  ./${name}\n`);
      writeFileSync(join(root, "nylzo_1.0.0.evidence.sha256.sigstore.json"), "{}");
      return spawnSync("bash", ["deploy/hetzner-flatcar/host/update/release-policy", "v1.0.0"], { encoding: "utf8", timeout: 10000,
        env: { ...process.env, PATH: `${root}/bin:${process.env.PATH}`, FIXTURE: root, ...overrides } });
    };
    const good = { format: 1, tag: "v1.0.0", source: "a".repeat(40), kind: "planned", policyCommit: "b".repeat(40) };
    const accepted = run(good); assert.equal(accepted.status, 0, accepted.stderr);
    assert.notEqual(run(good, { SIGNATURE_STATUS: "1" }).status, 0);
    assert.notEqual(run(good, { CORRUPT: "true" }).status, 0);
    for (const changed of [{ ...good, kind: "weekly" }, { ...good, kind: "security" }, { ...good, tag: "v1.0.1" }, { ...good, source: "unknown" }, { ...good, policyCommit: null }]) assert.notEqual(run(changed).status, 0);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
