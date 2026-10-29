import assert from "node:assert/strict";
import { releaseBranch } from "../release/policy.mts";
import { repository, request } from "../release/github.mts";

assert.equal(process.env.GITHUB_REPOSITORY, repository);
assert.equal(process.env.GITHUB_REF, "refs/heads/main");
const latest = request(`repos/${repository}/releases/latest`);
assert.equal(latest.immutable, true);
const reference = request(`repos/${repository}/git/ref/tags/${latest.tag_name}`);
assert.equal(reference.object.type, "tag");
const tag = request(`repos/${repository}/git/tags/${reference.object.sha}`);
assert.equal(tag.object.type, "commit");
const branches = request(`repos/${repository}/git/matching-refs/heads/${releaseBranch(latest.tag_name)}`);
const branch = branches.find((item: any) => item.ref === `refs/heads/${releaseBranch(latest.tag_name)}`);
if (branch && branch.object.sha !== tag.object.sha) {
  assert.match(branch.object.sha, /^[a-f0-9]{40}$/u);
  const commit = request(`repos/${repository}/commits/${branch.object.sha}`);
  const created = Date.parse(commit.commit.committer.date);
  assert(Number.isFinite(created) && created <= Date.now() + 60000, "Invalid maintenance commit time");
  if (Date.now() - created > 60 * 60 * 1000) {
    // Publication may have completed between our metadata reads.
    const current = request(`repos/${repository}/releases/latest`);
    assert(current.tag_name !== latest.tag_name, "A merged maintenance update has not published within one hour. Inspect CI and the release queue.");
  }
}
console.log("No stalled maintenance publication.");
