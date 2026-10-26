import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { buildInfo } from "./build-info.mts";

test("release, snapshot, dirty, and source-archive metadata remain distinguishable", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-build-info-"));
  const git = (...arguments_: string[]) =>
    execFileSync("git", arguments_, { cwd: directory, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  try {
    assert.deepEqual(buildInfo(directory), { version: "0.0.0-dev.dirty", commit: "", date: "", dirty: true });
    git("init", "-q");
    git("config", "user.email", "test@example.invalid");
    git("config", "user.name", "Build test");
    writeFileSync(join(directory, "source.txt"), "synthetic source\n");
    git("add", "source.txt");
    git("-c", "commit.gpgsign=false", "commit", "-qm", "feat: initial source");
    const commit = git("rev-parse", "HEAD");
    const development = buildInfo(directory);
    assert.equal(development.version, `0.0.0-dev.g${commit.slice(0, 12)}`);
    assert.equal(development.commit, commit);
    assert.equal(development.dirty, false);
    assert.ok(!Number.isNaN(Date.parse(development.date)));
    git("-c", "tag.gpgsign=false", "tag", "v1.2.3");
    assert.equal(buildInfo(directory).version, "1.2.3");
    assert.equal(buildInfo(directory, "1.2.4-snapshot").version, "1.2.4-snapshot");
    writeFileSync(join(directory, "source.txt"), "modified\n");
    assert.equal(buildInfo(directory).version, `0.0.0-dev.g${commit.slice(0, 12)}.dirty`);
    assert.equal(buildInfo(directory).dirty, true);
    for (const invalid of ["01.2.3", "1.2.3-01", "1.2.3-..", "1.2.3\nextra", "$(command)", ""]) {
      if (invalid !== "") assert.throws(() => buildInfo(directory, invalid), /semantic version/);
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
