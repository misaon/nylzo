import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import test from "node:test";

type Step = { env?: Record<string, string>; run?: string };

const steps = (Bun.YAML.parse(readFileSync(".github/actions/setup/action.yml", "utf8")) as { runs: { steps: Step[] } }).runs.steps;
const guard = steps.find((step) => step.env?.DEFAULT_BRANCH !== undefined)?.run ?? "";

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
