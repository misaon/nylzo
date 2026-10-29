import assert from "node:assert/strict";
import { appendFileSync } from "node:fs";
import { pages, repository, request } from "../release/github.mts";
import { releaseBranch, trustedDependencyBot, versionParts } from "../release/policy.mts";

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
const branch = releaseBranch(latest.tag_name);
const branches = request(`repos/${repository}/git/matching-refs/heads/${branch}`);
const existing = branches.find((item: any) => item.ref === `refs/heads/${branch}`);
if (!existing) request(`repos/${repository}/git/refs`, "POST", { ref: `refs/heads/${branch}`, sha });
else if (existing.object.sha !== sha) {
  appendFileSync(process.env.GITHUB_OUTPUT!, "ready=false\n");
  console.log("A verified update is awaiting publication; discovery resumes on the next published baseline.");
  process.exit(0);
}
// Discard only superseded machine proposals, keeping migration/failed-test PRs
// assigned to the maintainer. Reopening a PR remains possible on GitHub.
const pulls = pages(`repos/${repository}/pulls?state=open&per_page=100`);
const blocked = pulls.filter(pull => trustedDependencyBot(pull.user) && pull.head.repo.full_name === repository && pull.labels.some((label: any) => label.name === "requires-attention")).length;
assert(blocked <= 1000);
for (const pull of pulls) {
  if (trustedDependencyBot(pull.user) && pull.head.repo.full_name === repository &&
      pull.head.ref.startsWith("renovate/") && pull.base.ref.startsWith("release/v") &&
      pull.base.ref !== branch && !pull.labels.some((label: any) => label.name === "requires-attention")) {
    request(`repos/${repository}/pulls/${pull.number}`, "PATCH", { state: "closed" });
  }
}
appendFileSync(process.env.GITHUB_OUTPUT!, `ready=true\ntag=${latest.tag_name}\nsource=${sha}\nbranch=${branch}\nblocked=${blocked}\n`);
console.log(`Isolated update base ${branch} points to the last immutable published source.`);
