import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdtemp, writeFile, chmod, symlink, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { privateFile, inputs } from "./operator.mts";

test("credential loading refuses readable or redirected secrets", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "nylzo-credentials-"));
  try {
    const filename = path.join(directory, "credential");
    await writeFile(filename, "synthetic-test-credential-not-a-live-token", { mode: 0o644 });
    await assert.rejects(privateFile(filename));
    await chmod(filename, 0o600);
    assert.equal(await privateFile(filename), "synthetic-test-credential-not-a-live-token");
    await symlink(filename, path.join(directory, "redirect"));
    await assert.rejects(privateFile(path.join(directory, "redirect")));
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("reviewed plans are bound to provider locks, infrastructure, variables and generated host configuration", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "nylzo-plan-"));
  try {
    for (const name of ["main.tf", ".terraform.lock.hcl", "deployment.auto.tfvars.json", "user-data"]) await writeFile(path.join(directory, name), "initial");
    let previous = await inputs(directory);
    for (const name of ["main.tf", ".terraform.lock.hcl", "deployment.auto.tfvars.json", "user-data"]) {
      await writeFile(path.join(directory, name), "changed");
      const next = await inputs(directory);
      assert.notEqual(next, previous);
      previous = next;
    }
  } finally { await rm(directory, { recursive: true, force: true }); }
});
