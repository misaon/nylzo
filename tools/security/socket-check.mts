import assert from "node:assert/strict";
import { setTimeout } from "node:timers/promises";
import { pages, repository } from "../release/github.mts";

const checkName = "Socket Security: Pull Request Alerts";
const appID = 156372;

export function socketResult(checks: any[], head: string): "pending" | "success" | "failure" {
  assert.match(head, /^[a-f0-9]{40}$/u);
  const current = checks.filter(check => check.name === checkName && check.app?.id === appID && check.head_sha === head)
    .sort((first, second) => second.id - first.id)[0];
  if (!current || current.status !== "completed") return "pending";
  return current.conclusion === "success" ? "success" : "failure";
}

if (import.meta.main) {
  assert.equal(process.env.GITHUB_REPOSITORY, repository);
  assert.equal(process.env.GITHUB_EVENT_NAME, "pull_request");
  const head = process.env.SOCKET_HEAD_SHA!;
  assert.match(head, /^[a-f0-9]{40}$/u);
  const deadline = Date.now() + 120_000;
  let passed = false;
  while (Date.now() < deadline) {
    const checks = pages(`repos/${repository}/commits/${head}/check-runs?check_name=${encodeURIComponent(checkName)}&filter=all&per_page=100`).flatMap(page => page.check_runs);
    const result = socketResult(checks, head);
    if (result === "success") { passed = true; console.log("The exact PR head passed the required Socket App check."); break; }
    assert.notEqual(result, "failure", "Socket rejected this exact PR head; inspect its findings before merging");
    await setTimeout(5_000);
  }
  assert(passed, "Socket has not completed; no merge is authorized");
}
