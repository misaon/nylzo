import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import test from "node:test";

const isRecord = (value: unknown): value is Record<string, unknown> => typeof value === "object" && value !== null && !Array.isArray(value);

function guardScript() {
  const action: unknown = Bun.YAML.parse(readFileSync(".github/actions/setup/action.yml", "utf8"));
  const runs = isRecord(action) ? action["runs"] : undefined;
  const steps = isRecord(runs) ? runs["steps"] : undefined;
  assert(Array.isArray(steps), "The setup action lists no steps");
  const step: unknown = steps.find((candidate: unknown) => isRecord(candidate) && isRecord(candidate["env"]) && candidate["env"]["DEFAULT_BRANCH"] !== undefined);
  return isRecord(step) && typeof step["run"] === "string" ? step["run"] : "";
}

const guard = guardScript();

function checkout(event: string, ref: string) {
  return spawnSync("bash", ["--noprofile", "--norc", "-eo", "pipefail", "-c", guard], {
    env: { ...process.env, EVENT: event, REF: ref, DEFAULT_BRANCH: "main" },
  }).status;
}

test("only runs of this repository's own code may check out another ref, and workflow_run only the default branch", () => {
  assert.match(guard, /case/u);
  for (const event of ["push", "pull_request", "schedule", "workflow_dispatch"]) assert.equal(checkout(event, "release/v1"), 0, event);
  for (const event of ["pull_request_target", "workflow_run", "issue_comment"]) assert.equal(checkout(event, ""), 0, event);
  assert.equal(checkout("workflow_run", "main"), 0);
  for (const [event, ref] of [
    ["pull_request_target", "main"],
    ["pull_request_target", "a".repeat(40)],
    ["workflow_run", "MAIN"],
    ["workflow_run", "refs/heads/main"],
    ["workflow_run", "renovate/main"],
    ["issue_comment", "main"],
  ] as const) {
    assert.equal(checkout(event, ref), 1, `${event} ${ref}`);
  }
});
