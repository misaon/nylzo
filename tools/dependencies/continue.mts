import assert from "node:assert/strict";
import { pages, repository, request } from "../release/github.mts";
import { blockedProposals } from "./queue.mts";

assert.equal(process.env.GITHUB_REPOSITORY, repository);
assert.equal(process.env.GITHUB_REF, "refs/heads/main");
assert.match(process.env.PREVIOUS_BLOCKED!, /^\d{1,4}$/u);
const previous = Number(process.env.PREVIOUS_BLOCKED);
assert(previous <= 1000);
const mode = process.env.NYLZO_UPDATE_MODE;
assert(mode === "security" || mode === "weekly");
const pulls = pages(`repos/${repository}/pulls?state=open&per_page=100`);
const tag = process.env.NYLZO_BASE_TAG ?? request(`repos/${repository}/releases/latest`).tag_name;
const blocked = blockedProposals(pulls, tag);
assert(blocked <= 1000);
if (blocked > previous) {
  // New major/migration proposals can arrive already labelled for attention.
  // They must not occupy all active slots until the next six-hour schedule.
  request(`repos/${repository}/actions/workflows/dependencies.yml/dispatches`, "POST", { ref: "main", inputs: { mode } });
  console.log("New manual proposals were recorded; continue discovery with their slots excluded.");
} else console.log("No new manual proposals; no additional discovery run is needed.");
