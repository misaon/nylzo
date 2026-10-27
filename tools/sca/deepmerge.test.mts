import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { checkPatchBinding } from "./deepmerge.mts";

test("deepmerge remediation requires the exact patch and every locked consumer", () => {
  const patch = readFileSync("web/patches/deepmerge@4.3.1.patch");
  const workspace = readFileSync("web/pnpm-workspace.yaml", "utf8");
  const lockfile = readFileSync("web/pnpm-lock.yaml", "utf8");
  checkPatchBinding(patch, workspace, lockfile);
  checkPatchBinding(patch, workspace.replaceAll("\n", "\r\n"), lockfile.replaceAll("\n", "\r\n"));
  assert.throws(() => checkPatchBinding(Buffer.concat([patch, Buffer.from("\n")] ), workspace, lockfile));
  assert.throws(() => checkPatchBinding(patch, workspace.replace("patchedDependencies:", "removedPatches:"), lockfile));
  assert.throws(() => checkPatchBinding(patch, workspace, lockfile.replace(/deepmerge: 4\.3\.1\(patch_hash=[a-f0-9]+\)/u, "deepmerge: 4.3.1")));
  assert.throws(() => checkPatchBinding(patch, workspace, `${lockfile}\n      deepmerge: 4.3.1\n`));
  assert.throws(() => checkPatchBinding(patch, workspace, `${lockfile}\n  deepmerge@4.3.1: {}\n`));
});
