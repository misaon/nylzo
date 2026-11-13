import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

test("the data volume step refuses to leave data unencrypted when the profile, Tang or an existing volume requires encryption", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-storage-"));
  try {
    const calls = join(directory, "calls");
    for (const command of ["clevis", "cryptsetup", "losetup", "fallocate", "mkfs.ext4", "mount", "mountpoint", "journalctl", "df"]) {
      writeFileSync(join(directory, command), `#!/bin/sh\necho ${command} >> "${calls}"\nexit 1\n`);
      chmodSync(join(directory, command), 0o700);
    }
    const file = (name: string, contents: string) => { const target = join(directory, name); writeFileSync(target, contents); return target; };
    const absent = join(directory, "absent");
    const tang = file("tang.json", '{"t":2,"pins":{"tang":[]}}');
    const image = file("nylzo-storage.luks", "");
    const run = (settings: string, storage: Record<string, string> = {}) => {
      rmSync(calls, { force: true });
      const result = spawnSync("bash", [resolve("deploy/hetzner-flatcar/host/application/storage")], { encoding: "utf8", env: {
        ...process.env, PATH: `${directory}:${process.env.PATH}`,
        NYLZO_HOST_SETTINGS: file("host.env", settings), NYLZO_TANG_CONFIGURATION: absent, NYLZO_STORAGE_IMAGE: absent, ...storage,
      } });
      let commands = "";
      try { commands = readFileSync(calls, "utf8"); } catch {}
      return { ...result, commands };
    };
    for (const settings of ["PUBLIC_ACCESS=false\n", "PUBLIC_ACCESS=false\nENCRYPTED_STORAGE_REQUIRED=false\n"]) {
      const plain = run(settings);
      assert.equal(plain.status, 0, plain.stderr);
      assert.match(plain.stderr, /unencrypted root filesystem/u);
      assert.equal(plain.commands, "");
    }
    for (const [settings, storage] of [
      ["ENCRYPTED_STORAGE_REQUIRED=true\n", {}],
      ["ENCRYPTED_STORAGE_REQUIRED=false\n", { NYLZO_STORAGE_IMAGE: image }],
      ["ENCRYPTED_STORAGE_REQUIRED=true\n", { NYLZO_TANG_CONFIGURATION: file("empty.json", "") }],
      ["PUBLIC_ACCESS=false\n", { NYLZO_TANG_CONFIGURATION: file("empty.json", "") }],
    ] as const) {
      const refused = run(settings, storage);
      assert.equal(refused.status, 1, JSON.stringify({ settings, storage }));
      assert.match(refused.stderr, /^Encrypted storage refused: .* is missing$/mu);
      assert.equal(refused.commands, "", "Nothing may be created or unlocked without the Tang configuration");
    }
    for (const settings of ["PUBLIC_ACCESS=false\n", "ENCRYPTED_STORAGE_REQUIRED=false\n"]) {
      const created = run(settings, { NYLZO_TANG_CONFIGURATION: tang });
      assert.notEqual(created.status, 0, "The stubs refuse every storage command");
      assert.doesNotMatch(created.stderr, /unencrypted root filesystem/u, "A host with Tang must create the volume");
    }
    const invalid = run("ENCRYPTED_STORAGE_REQUIRED=yes\n", { NYLZO_TANG_CONFIGURATION: tang });
    assert.equal(invalid.status, 1);
    assert.match(invalid.stderr, /set ENCRYPTED_STORAGE_REQUIRED to true or false/u);
    const unlock = run("ENCRYPTED_STORAGE_REQUIRED=true\n", { NYLZO_TANG_CONFIGURATION: tang, NYLZO_STORAGE_IMAGE: image });
    assert.notEqual(unlock.status, 0, "The stubs refuse every storage command");
    assert.equal(unlock.commands, "losetup\n", "With Tang configured the step must unlock the existing volume");
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

test("a retry of volume creation first releases the half-created volume file", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-storage-retry-"));
  try {
    const calls = join(directory, "calls");
    writeFileSync(join(directory, "losetup"), `#!/bin/sh\necho "losetup $*" >> "${calls}"\ncase "$*" in *--associated*) echo /dev/loop9 ;; esac\n`);
    writeFileSync(join(directory, "df"), `#!/bin/sh\necho df >> "${calls}"\nexit 1\n`);
    for (const command of ["losetup", "df"]) chmodSync(join(directory, command), 0o700);
    const image = join(directory, "nylzo-storage.luks");
    writeFileSync(`${image}.new`, "half-created");
    writeFileSync(join(directory, "tang.json"), '{"t":2,"pins":{"tang":[]}}');
    writeFileSync(join(directory, "host.env"), "ENCRYPTED_STORAGE_REQUIRED=true\n");
    const result = spawnSync("bash", [resolve("deploy/hetzner-flatcar/host/application/storage")], { encoding: "utf8", env: {
      ...process.env, PATH: `${directory}:${process.env.PATH}`,
      NYLZO_HOST_SETTINGS: join(directory, "host.env"), NYLZO_TANG_CONFIGURATION: join(directory, "tang.json"), NYLZO_STORAGE_IMAGE: image,
    } });
    assert.notEqual(result.status, 0);
    assert.deepEqual(readFileSync(calls, "utf8").split("\n").slice(0, 2), [`losetup --noheadings --output NAME --associated ${image}.new`, "losetup --detach /dev/loop9"]);
    assert(!existsSync(`${image}.new`), "The free-space check must not count a leftover volume file");
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
