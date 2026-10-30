import assert from "node:assert/strict";
import { appendFileSync } from "node:fs";
import { contents, pages, repository, request } from "../release/github.mts";
import { releaseBranch, supportsToolInventory, trustedDependencyBot, versionParts } from "../release/policy.mts";
import { blockedProposals } from "./queue.mts";
import { batchRecords, discoveryPlan } from "./batch-state.mts";
import batches from "./batches.cjs";

assert.equal(process.env.GITHUB_REPOSITORY, repository);
assert.equal(process.env.GITHUB_REF, "refs/heads/main");
const latest = request(`repos/${repository}/releases/latest`);
assert.equal(latest.immutable, true); assert.equal(latest.draft, false); assert.equal(latest.prerelease, false);
versionParts(latest.tag_name);
const reference = request(`repos/${repository}/git/ref/tags/${latest.tag_name}`);
assert.equal(reference.object.type, "tag");
const tag = request(`repos/${repository}/git/tags/${reference.object.sha}`);
assert.equal(tag.object.type, "commit");
const sha = tag.object.sha;
assert.match(sha, /^[a-f0-9]{40}$/u);
if (!supportsToolInventory(JSON.parse(contents("tools/dependencies/assets.json", sha)))) {
  appendFileSync(process.env.GITHUB_OUTPUT!, "ready=false\n");
  console.log("Publish the approved scanner bootstrap release before automatic maintenance updates.");
  process.exit(0);
}
const branch = releaseBranch(latest.tag_name);
const branches = request(`repos/${repository}/git/matching-refs/heads/${branch}`);
const existing = branches.find((item: any) => item.ref === `refs/heads/${branch}`);
if (!existing) request(`repos/${repository}/git/refs`, "POST", { ref: `refs/heads/${branch}`, sha });
else if (existing.object.sha !== sha) {
  appendFileSync(process.env.GITHUB_OUTPUT!, "ready=false\n");
  console.log("A verified update is awaiting publication; discovery resumes on the next published baseline.");
  process.exit(0);
}
// Close superseded machine proposals before budgeting. Preserve human edits;
// a closed PR retains its diff and history and can be reopened for review.
const pulls = pages(`repos/${repository}/pulls?state=open&per_page=100`);
const main = request(`repos/${repository}/git/ref/heads/main`).object.sha;
const records = batchRecords(pulls);
for (const record of batches.staleEvidence(records, { main, [branch]: sha })) {
  const current = request(`repos/${repository}/pulls/${record.number}`);
  const labels = current.labels.map((label: any) => label.name);
  if (current.state !== "open" || current.head.sha !== record.head || current.draft || labels.includes("manual-hold") || (labels.includes("requires-attention") && !labels.includes("automation:ci-failed"))) continue;
  request(`repos/${repository}/pulls/${record.number}`, "PATCH", { state: "closed" });
  pulls.find(pull => pull.number === record.number).state = "closed";
  console.log(`Closed consumed or superseded batch evidence PR #${record.number}; individual failures and human holds remain reviewable.`);
}
for (const pull of pulls) {
  if (pull.state === "open" && trustedDependencyBot(pull.user) && pull.head.repo.full_name === repository &&
      pull.head.ref.startsWith("renovate/") && pull.base.ref.startsWith("release/v") &&
      pull.base.ref !== branch) {
    // Failed diagnostic snapshots retain exact-version quarantine evidence.
    const held = pull.labels.some((label: any) => label.name === "requires-attention" || label.name === "manual-hold");
    if (["diagnostic", "regroup"].includes(batches.branchPhase(pull.head.ref)) && held) continue;
    const commits = pages(`repos/${repository}/pulls/${pull.number}/commits?per_page=100`);
    if (commits.length !== 1 || commits[0].sha !== pull.head.sha || !trustedDependencyBot(commits[0].author) || !commits[0].commit.verification.verified || commits[0].parents.length !== 1) continue;
    if (held && /-dependency-batch$/u.test(pull.head.ref)) {
      try {
        const batch = batches.readBatch(commits[0].commit.message ?? "");
        if (batch && batches.units(batch.candidates).size === 1) continue;
        if (batch && batchRecords([pull])[0]?.unavailable.length) continue;
      } catch { continue; }
    }
    request(`repos/${repository}/pulls/${pull.number}`, "PATCH", { state: "closed" });
    pull.state = "closed";
  }
}
const blocked = blockedProposals(pulls);
assert(blocked <= 1000);
const mode = process.env.NYLZO_UPDATE_MODE;
let isolation: any = { plans: {}, excluded: [] };
if (mode === "security" || mode === "weekly") {
  const open = pulls.filter(pull => pull.state === "open"), numbers = new Set(open.map(pull => pull.number));
  isolation = discoveryPlan(open, branch, sha, main, mode, records.filter(record => numbers.has(record.number)));
}
appendFileSync(process.env.GITHUB_OUTPUT!, `batch_plan=${JSON.stringify(isolation)}\n`);
appendFileSync(process.env.GITHUB_OUTPUT!, `ready=true\ntag=${latest.tag_name}\nsource=${sha}\nbranch=${branch}\nblocked=${blocked}\n`);
console.log(`Isolated update base ${branch} points to the last immutable published source.`);
