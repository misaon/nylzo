import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const deepmergePatch = {
  id: "GHSA-gcqx-w48m-w2h4", ecosystem: "npm", package: "deepmerge", version: "4.3.1",
  sha256: "c2cb33d5ff7ad8e752bdd9ec3a8e8e7520ad9b4da374307a6633372158ece14e",
};

const hashes = {
  "index.js": "1799463abfdaef66f8ac03a90ec7bc9e5deadd968ea2ffe5ca7b6f057888e36e",
  "dist/cjs.js": "371951d3ebefbb374911bb51e763c08bf8523e55c2404eeb1aaea73211b00bbb",
  "dist/umd.js": "0aad2da89244af1b0aad6f57c60952eccac23d8dedb90593ddcbd11a7c2bd07f",
};

export function checkPatchBinding(patch: Buffer, workspace: string, lockfile: string) {
  assert.equal(createHash("sha256").update(patch).digest("hex"), deepmergePatch.sha256, "Unreviewed deepmerge patch");
  assert.match(workspace, /^patchedDependencies:\n  deepmerge@4\.3\.1: patches\/deepmerge@4\.3\.1\.patch$/mu);
  assert.match(lockfile, new RegExp(`^patchedDependencies:\\n  deepmerge@4\\.3\\.1: ${deepmergePatch.sha256}$`, "mu"));
  const references = [...lockfile.matchAll(/^\s+deepmerge: (.+)$/gmu)].map(match => match[1]);
  assert.deepEqual(references, [`4.3.1(patch_hash=${deepmergePatch.sha256})`], "Every deepmerge consumer must use the reviewed patch");
  assert(!/^  deepmerge@4\.3\.1: \{\}$/mu.test(lockfile), "Unpatched deepmerge snapshot");
}

export function checkMerge(merge: any) {
  const payload = () => JSON.parse('{"__proto__":{"nylzoPoisoned":true},"safe":1}');
  const targets = [Object.create(null), { nested: null }, { nested: undefined }, JSON.parse('{"__proto__":{}}')];
  for (const target of targets) {
    for (const result of [merge(target, payload()), merge(target, { nested: payload() }), merge(payload(), target), merge.all([payload(), target])]) {
      assert.equal(result.nylzoPoisoned, undefined, "Returned object prototype was poisoned");
      assert(!Object.hasOwn(result, "__proto__"));
      if (result.nested && typeof result.nested === "object") {
        assert.equal(result.nested.nylzoPoisoned, undefined, "Nested object prototype was poisoned");
        assert(!Object.hasOwn(result.nested, "__proto__"));
      }
    }
  }
  assert.equal(({} as any).nylzoPoisoned, undefined);
  const symbol = Symbol("option"), callback = () => {}, regex = /svelte/u, date = new Date(0);
  const options = { arrayMerge: (target: any, source: any) => source ?? target };
  const original = { nested: { a: 1 }, list: [1], callback, regex, date, [symbol]: true };
  const result = merge(original, { nested: { b: 2 }, list: [2] }, options);
  assert.deepEqual(result.nested, { a: 1, b: 2 });
  assert.notEqual(result.nested, original.nested);
  assert.deepEqual(result.list, [2]);
  for (const key of ["callback", "regex", "date", symbol]) assert.equal(result[key], (original as any)[key]);
  assert.deepEqual(original.list, [1]);
}

export function verifyDeepmergePatch(root = fileURLToPath(new URL("../../", import.meta.url))) {
  checkPatchBinding(readFileSync(resolve(root, "web/patches/deepmerge@4.3.1.patch")), readFileSync(resolve(root, "web/pnpm-workspace.yaml"), "utf8"), readFileSync(resolve(root, "web/pnpm-lock.yaml"), "utf8"));
  // Resolve through the real consumer, rather than trusting an unrelated top-level package.
  const appRequire = createRequire(resolve(root, "web/app/package.json"));
  const pluginRequire = createRequire(appRequire.resolve("@sveltejs/vite-plugin-svelte"));
  const entry = pluginRequire.resolve("deepmerge");
  const directory = dirname(dirname(entry));
  assert.equal(entry, resolve(directory, "dist/cjs.js"), "Unexpected deepmerge entry point");
  assert.equal(JSON.parse(readFileSync(resolve(directory, "package.json"), "utf8")).version, deepmergePatch.version);
  for (const [file, hash] of Object.entries(hashes)) {
    assert.equal(createHash("sha256").update(readFileSync(resolve(directory, file))).digest("hex"), hash, `Unpatched or changed deepmerge ${file}`);
  }
  checkMerge(pluginRequire("deepmerge"));
  checkMerge(pluginRequire(resolve(directory, "dist/umd.js")));
  return { ...deepmergePatch, files: hashes };
}
