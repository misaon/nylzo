import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";

type Assessment = { id: string; lockfile: string; reviewUntil: string };
type IgnoredVulnerability = { id: string; ignoreUntil: Temporal.Instant };

const root = join(import.meta.dirname, "../..");
const isRecord = (value: unknown): value is Record<string, unknown> => typeof value === "object" && value !== null && !Array.isArray(value);
const isAssessment = (value: unknown): value is Assessment =>
  isRecord(value) && typeof value["id"] === "string" && typeof value["lockfile"] === "string" && typeof value["reviewUntil"] === "string";
const isIgnoredVulnerability = (value: unknown): value is IgnoredVulnerability =>
  isRecord(value) && typeof value["id"] === "string" && value["ignoreUntil"] instanceof Temporal.Instant;

function assessments(): Assessment[] {
  const policy: unknown = JSON.parse(readFileSync(join(root, "security/sca-policy.json"), "utf8"));
  const notAffected = isRecord(policy) ? policy["notAffected"] : undefined;
  assert(Array.isArray(notAffected) && notAffected.every(isAssessment), "security/sca-policy.json lists no assessments");
  return notAffected;
}

function ignoredIn(file: string): string[] {
  const configuration: unknown = Bun.TOML.parse(readFileSync(join(root, file), "utf8"));
  const ignored = isRecord(configuration) ? (configuration["IgnoredVulns"] ?? []) : undefined;
  assert(Array.isArray(ignored) && ignored.every(isIgnoredVulnerability), `${file} lists a vulnerability without an id or an ignoreUntil instant`);
  return ignored.map(vulnerability => `${vulnerability.id} until ${vulnerability.ignoreUntil.toString()}`).toSorted();
}

test("Scorecard ignores exactly the vulnerabilities the SCA policy assessed, until their review dates", () => {
  const expected = new Map<string, string[]>();
  for (const assessment of assessments()) {
    const file = join(dirname(assessment.lockfile), "osv-scanner.toml");
    expected.set(file, [...(expected.get(file) ?? []), `${assessment.id} until ${Temporal.Instant.from(assessment.reviewUntil).toString()}`].toSorted());
  }
  const files = execFileSync("git", ["ls-files", "*osv-scanner.toml"], { cwd: root, encoding: "utf8" }).trim().split("\n");
  assert.deepEqual(ignoredIn("security/osv-scanner.toml"), []);
  assert.deepEqual(
    new Map(files.filter(file => file !== "security/osv-scanner.toml").map(file => [file, ignoredIn(file)])),
    expected,
  );
});
