import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

test("publication rebuilds source without promoting cached CI artifacts", () => {
  const release = readFileSync(".github/workflows/release.yml", "utf8");
  assert(!release.includes("actions/cache/"));
  assert(!release.includes("pattern: hashes-"));
  assert(!release.includes("name: web-dist"));
  assert(release.includes("pnpm --dir web --filter @nylzo/web build"));
  assert(!/cache: (?:pnpm|true)/u.test(release));
  assert(release.includes("package-manager-cache: false"));
});

test("a release requires full verification and successful security checks from the same run", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-release-checks-"));
  try {
    const executable = join(directory, "gh");
    writeFileSync(executable, '#!/bin/sh\nprintf "%s" "$CHECKS"\n');
    chmodSync(executable, 0o700);
    const names = [
      "Release verification",
      "CI required",
      "Analyze (go)",
      "Analyze (javascript-typescript)",
      "Analyze (actions)",
    ];
    const checks = names.map((name) => ({
      name,
      app: { slug: "github-actions" },
      conclusion: "success",
      check_suite: { id: 1 },
    }));
    const verify = (checkRuns: typeof checks) =>
      spawnSync("bash", ["mise-tasks/release/checks", "a".repeat(40)], {
        env: {
          ...process.env,
          PATH: `${directory}:${process.env.PATH}`,
          CHECKS: JSON.stringify({ check_runs: checkRuns }),
        },
        encoding: "utf8",
      });
    assert.equal(verify(checks).status, 0);
    for (let index = 0; index < checks.length; index++) {
      assert.notEqual(verify(checks.filter((_, position) => position !== index)).status, 0);
      for (const conclusion of ["skipped", "cancelled", "failure"]) {
        assert.notEqual(
          verify(checks.map((check, position) => (position === index ? { ...check, conclusion } : check))).status,
          0,
        );
      }
      assert.notEqual(
        verify(checks.map((check, position) => (position === index ? { ...check, check_suite: { id: 2 } } : check)))
          .status,
        0,
      );
      assert.notEqual(
        verify(
          checks.map((check, position) => (position === index ? { ...check, app: { slug: "untrusted-app" } } : check)),
        ).status,
        0,
      );
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
