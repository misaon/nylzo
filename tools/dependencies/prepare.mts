import assert from "node:assert/strict";
import { appendFileSync } from "node:fs";
import { contents, pages, repository, request } from "../release/github.mts";
import { releaseBranch, supportsToolInventory, trustedDependencyBot, versionParts } from "../release/policy.mts";
import { blockedProposals } from "./queue.mts";

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
for (const pull of pulls) {
  if (trustedDependencyBot(pull.user) && pull.head.repo.full_name === repository &&
      pull.head.ref.startsWith("renovate/") && pull.base.ref.startsWith("release/v") &&
      pull.base.ref !== branch) {
    const commits = pages(`repos/${repository}/pulls/${pull.number}/commits?per_page=100`);
    if (commits.length !== 1 || commits[0].sha !== pull.head.sha || !trustedDependencyBot(commits[0].author) || !commits[0].commit.verification.verified || commits[0].parents.length !== 1) continue;
    request(`repos/${repository}/pulls/${pull.number}`, "PATCH", { state: "closed" });
    pull.state = "closed";
  }
}
const blocked = blockedProposals(pulls);
assert(blocked <= 1000);
appendFileSync(process.env.GITHUB_OUTPUT!, `ready=true\ntag=${latest.tag_name}\nsource=${sha}\nbranch=${branch}\nblocked=${blocked}\n`);
console.log(`Isolated update base ${branch} points to the last immutable published source.`);
