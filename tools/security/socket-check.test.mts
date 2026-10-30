import assert from "node:assert/strict";
import test from "node:test";
import { socketResult } from "./socket-check.mts";

const head = "a".repeat(40);
const passed = { id: 1, head_sha: head, app: { id: 156372 }, name: "Socket Security: Pull Request Alerts", status: "completed", conclusion: "success" };

test("only the newest matching Socket App check on the exact PR head can pass", () => {
  assert.equal(socketResult([passed], head), "success");
  for (const changed of [[], [{ ...passed, app: { id: 15368 } }], [{ ...passed, head_sha: "b".repeat(40) }], [{ ...passed, name: "Socket Security: Project Report" }], [{ ...passed, status: "in_progress", conclusion: null }]]) {
    assert.equal(socketResult(changed, head), "pending");
  }
  for (const conclusion of ["failure", "neutral", "skipped", "cancelled", "timed_out", "action_required"]) {
    assert.equal(socketResult([passed, { ...passed, id: 2, conclusion }], head), "failure");
  }
  assert.equal(socketResult([passed, { ...passed, id: 2, status: "in_progress", conclusion: null }], head), "pending", "An earlier success cannot bypass an unfinished replacement");
});
