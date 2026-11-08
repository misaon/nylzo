import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";

type Assessment = { id: string; lockfile: string; reviewUntil: string };
type IgnoredVulnerability = { id: string; ignoreUntil: Temporal.Instant };

const root = join(import.meta.dirname, "../..");
const policy = JSON.parse(readFileSync(join(root, "security/sca-policy.json"), "utf8")) as { fixed: Assessment[]; notAffected: Assessment[] };

function ignoredIn(file: string): string[] {
  const { IgnoredVulns = [] } = Bun.TOML.parse(readFileSync(join(root, file), "utf8")) as { IgnoredVulns?: IgnoredVulnerability[] };
  return IgnoredVulns.map(vulnerability => `${vulnerability.id} until ${vulnerability.ignoreUntil.toString()}`).sort();
}

test("Scorecard ignores exactly the vulnerabilities the SCA policy assessed, until their review dates", () => {
  const expected = new Map<string, string[]>();
  for (const assessment of [...policy.fixed, ...policy.notAffected]) {
    const file = join(dirname(assessment.lockfile), "osv-scanner.toml");
    expected.set(file, [...(expected.get(file) ?? []), `${assessment.id} until ${Temporal.Instant.from(assessment.reviewUntil).toString()}`].sort());
  }
  const files = execFileSync("git", ["ls-files", "*osv-scanner.toml"], { cwd: root, encoding: "utf8" }).trim().split("\n");
  assert.deepEqual(ignoredIn("security/osv-scanner.toml"), []);
  assert.deepEqual(
    new Map(files.filter(file => file !== "security/osv-scanner.toml").map(file => [file, ignoredIn(file)])),
    expected,
  );
});
