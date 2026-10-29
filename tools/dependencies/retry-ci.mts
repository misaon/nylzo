import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { inspectDependencyPull, requireAttention } from "./inspect.mts";
import { releaseBranch, trustedDependencyBot } from "../release/policy.mts";
import { repository, request } from "../release/github.mts";

assert.equal(process.env.GITHUB_REPOSITORY, repository);
assert.equal(process.env.GITHUB_REF, "refs/heads/main");
const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH!, "utf8"));
const run = request(`repos/${repository}/actions/runs/${event.workflow_run.id}`);
assert.equal(run.workflow_id, request(`repos/${repository}/actions/workflows/ci.yml`).id);
assert.equal(run.head_repository.full_name, repository);
if (run.event !== "push" || run.status !== "completed" || !["failure", "timed_out"].includes(run.conclusion)) process.exit(0);
const latest = request(`repos/${repository}/releases/latest`);
assert.equal(latest.immutable, true);
if (run.head_branch !== releaseBranch(latest.tag_name)) process.exit(0);
assert.match(run.head_sha, /^[a-f0-9]{40}$/u);
if (request(`repos/${repository}/git/ref/heads/${run.head_branch}`).object.sha !== run.head_sha) process.exit(0);
const pulls = request(`repos/${repository}/commits/${run.head_sha}/pulls`);
const pull = pulls.find((item: any) => item.merged_at && item.merge_commit_sha === run.head_sha && item.base.ref === run.head_branch && trustedDependencyBot(item.user));
if (!pull) process.exit(0);
try {
  const update = inspectDependencyPull(pull.number, true, latest);
  const reference = request(`repos/${repository}/git/ref/tags/${latest.tag_name}`);
  assert.equal(reference.object.type, "tag");
  const tag = request(`repos/${repository}/git/tags/${reference.object.sha}`);
  assert.equal(tag.object.type, "commit");
  assert.equal(update.base, tag.object.sha, "Retry only an isolated update from the published source");
}
catch {
  requireAttention(pull, "The merged maintenance update needs review. No release or automatic CI retry attempted.");
  process.exit(0);
}
assert(Number.isSafeInteger(run.run_attempt) && run.run_attempt >= 1);
if (run.run_attempt < 3) {
  // Rerun every job, so the publication guard still sees one complete attempt.
  request(`repos/${repository}/actions/runs/${run.id}/rerun`, "POST");
  console.log(`Requested complete CI attempt ${run.run_attempt + 1}/3 for the same reviewed maintenance commit.`);
} else requireAttention(pull, "Post-merge CI failed after three complete attempts. Production remains on the published release.");
