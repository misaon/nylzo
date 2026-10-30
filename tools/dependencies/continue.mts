import assert from "node:assert/strict";
import { pages, repository, request } from "../release/github.mts";
import { blockedProposals } from "./queue.mts";
import { batchRecords } from "./batch-state.mts";
import batches from "./batches.cjs";
import { trustedDependencyBot } from "../release/policy.mts";

assert.equal(process.env.GITHUB_REPOSITORY, repository);
assert.equal(process.env.GITHUB_REF, "refs/heads/main");
assert.match(process.env.PREVIOUS_BLOCKED!, /^\d{1,4}$/u);
const previous = Number(process.env.PREVIOUS_BLOCKED);
assert(previous <= 1000);
const mode = process.env.NYLZO_UPDATE_MODE;
assert(mode === "security" || mode === "weekly");
const pulls = pages(`repos/${repository}/pulls?state=open&per_page=100`);
const blocked = blockedProposals(pulls);
assert(blocked <= 1000);
let unavailable = false;
const snapshot = JSON.parse(process.env.NYLZO_BATCH_PLAN || '{"plans":{}}');
if (process.env.RENOVATE_RESULT === "success" && Object.keys(snapshot.plans).length) {
  const records = batchRecords(pulls);
  // A completed lookup with a free proposal slot must produce a diagnostic
  // or explicitly record its unavailable target (for example a moved digest).
  const active = pulls.filter(pull => trustedDependencyBot(pull.user) && pull.state === "open" && !pull.draft && !pull.labels.some((label: any) => ["requires-attention", "manual-hold", "automation:diagnostic-passed"].includes(label.name))).length;
  if (active < 2) for (const plan of Object.values(snapshot.plans) as any[]) {
    if (plan.phase !== "diagnostic") continue;
    const root = records.find(record => record.number === plan.root && record.head === plan.head && record.batch.phase === "batch" && plan.candidates.every((item: any) => batches.keys(record.batch.candidates).includes(batches.key(item))));
    if (!root) continue;
    const missing = [...batches.units(plan.candidates)].filter(([id, candidates]) => !root.unavailable.includes(id) && !records.some(record => record.batch.phase === "diagnostic" && record.batch.root === root.number && record.batch.head === root.head && batches.same(record.batch.candidates, candidates)));
    if (!missing.length) continue;
    const data = { head: root.head, units: missing.map(([id]) => id) };
    request(`repos/${repository}/issues/${root.number}/comments`, "POST", { body: `<!-- nylzo-batch-unavailable-v1 ${JSON.stringify(data)} -->\nThe completed protected-main discovery could not prepare these exact diagnostic targets despite a free proposal slot: ${missing.flatMap(([, candidates]) => candidates.map(item => item.name)).join(", ")}. They remain for maintainer attention; verified updates can be regrouped. No untested target is approved for publication.` });
    unavailable = true;
  }
}
if (blocked > previous || unavailable) {
  // New major/migration proposals can arrive already labelled for attention.
  // They must not occupy all active slots until the next six-hour schedule.
  request(`repos/${repository}/actions/workflows/dependencies.yml/dispatches`, "POST", { ref: "main", inputs: { mode } });
  console.log("New manual proposals were recorded; continue discovery with their slots excluded.");
} else console.log("No new manual proposals; no additional discovery run is needed.");
