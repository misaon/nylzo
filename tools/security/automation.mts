import assert from "node:assert/strict";
import { appendFileSync, readFileSync } from "node:fs";
import { inspectDependencyPull, requireAttention, UpdateDeferred } from "../dependencies/inspect.mts";
import { contents, git, pages, repository, request } from "../release/github.mts";
import { nextVersion, releaseBranch, releaseMetadata, versionParts, trustedDependencyBot } from "../release/policy.mts";
import { changesAfter, lastMainRelease } from "../release/baseline.mts";

assert.equal(process.env.GITHUB_REPOSITORY, repository);
assert.equal(process.env.GITHUB_REF, "refs/heads/main");
assert.equal(process.env.SECURITY_AUTOMATION_ENABLED, "true");
const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH!, "utf8"));
const run = request(`repos/${repository}/actions/runs/${event.workflow_run.id}`);
assert.equal(run.workflow_id, request(`repos/${repository}/actions/workflows/ci.yml`).id);
assert.equal(run.head_repository.full_name, repository);
const allJobs = pages(`repos/${repository}/actions/runs/${run.id}/jobs?per_page=100`).flatMap(page => page.jobs ?? []);
const verified = ["Release verification", "CI required", "Analyze (go)", "Analyze (javascript-typescript)", "Analyze (actions)"].every(name => allJobs.some(job => job.name === name && job.conclusion === "success"));
const latest = request(`repos/${repository}/releases/latest`);
assert.equal(latest.immutable, true); assert.equal(latest.draft, false); assert.equal(latest.prerelease, false);
versionParts(latest.tag_name);

if (process.argv[2] === "merge") {
  assert.equal(run.event, "pull_request");
  const pulls = request(`repos/${repository}/commits/${run.head_sha}/pulls`);
  const pull = pulls.find((candidate: any) => candidate.head.sha === run.head_sha && candidate.state === "open" && trustedDependencyBot(candidate.user) && candidate.head.ref.startsWith("renovate/"));
  if (!pull) { console.log("No current automation dependency PR for this run."); process.exit(0); }
  let update;
  try { update = inspectDependencyPull(pull.number, false, latest); }
  catch (error) {
    if (error instanceof UpdateDeferred) console.log(error.message);
    else requireAttention(pull, "The update changes code, requires a migration, or has unsafe versions or integrity metadata. Review the diff and CI.");
    process.exit(0);
  }
  if (run.conclusion !== "success" || !verified) {
    if (["failure", "timed_out", "action_required"].includes(run.conclusion)) requireAttention(pull, "CI or a security check failed. No merge or release attempted.");
    process.exit(0);
  }
  assert.equal(update.pull.head.sha, run.head_sha);
  if (pull.base.ref !== "main") {
    const reference = request(`repos/${repository}/git/ref/tags/${latest.tag_name}`);
    const baseline = request(`repos/${repository}/git/tags/${reference.object.sha}`).object.sha;
    if (update.base !== baseline) { console.log("A verified patch is awaiting publication; do not stack another change onto it."); process.exit(0); }
  }
  if (pull.labels.some((label: any) => label.name === "requires-attention")) { console.log("Maintainer attention label prevents automatic merge."); process.exit(0); }
  const result = request(`repos/${repository}/pulls/${pull.number}/merge`, "PUT", {
    sha: run.head_sha, merge_method: "squash", commit_title: pull.title,
    commit_message: `Verified dependency-only update from PR #${pull.number}. Full CI and CodeQL passed on ${run.head_sha}.`,
  });
  assert.equal(result.merged, true);
  console.log(`Merged verified dependency PR #${pull.number}. Ordinary main commits do not trigger deployment.`);
} else {
  assert.equal(process.argv[2], "release");
  assert.equal(run.event, "push"); assert.equal(run.conclusion, "success"); assert(verified);
  assert(allJobs.some(job => job.name === "Release verification" && job.conclusion === "success"));
  const source = run.head_sha;
  assert.match(source, /^[a-f0-9]{40}$/u);
  assert.equal(request(`repos/${repository}/git/ref/heads/${run.head_branch}`).object.sha, source, "Ref changed after CI");
  assert.equal(git("rev-parse", "HEAD"), source);
  const commits = request(`repos/${repository}/commits/${source}/pulls`);
  const pull = commits.find((candidate: any) => candidate.merged_at && candidate.merge_commit_sha === source && candidate.base.ref === run.head_branch);
  if (!pull) { console.log("No merged release or dependency PR. Nothing to publish."); process.exit(0); }
  let tag: string, kind: string;
  if (run.head_branch === "main") {
    if (!trustedDependencyBot(pull.user) || pull.head.ref !== "release-please--branches--main") { console.log("Changes remain pending in the planned release PR."); process.exit(0); }
    const detail = request(`repos/${repository}/pulls/${pull.number}`);
    tag = releaseMetadata(detail, pages(`repos/${repository}/pulls/${pull.number}/files?per_page=100`), contents(".release-please-manifest.json", source));
    const baseline = lastMainRelease();
    const messages = changesAfter(baseline.sha);
    assert.equal(tag, `v${nextVersion(latest.tag_name, messages)}`, "Release PR is stale; regenerate instead of publishing a colliding version");
    kind = "planned";
  } else {
    assert.equal(run.head_branch, releaseBranch(latest.tag_name));
    const update = inspectDependencyPull(pull.number, true, latest);
    const baseline = git("rev-parse", `${latest.tag_name}^{commit}`);
    assert.equal(update.base, baseline, "Automatic patch must contain exactly one verified update from the last published source");
    assert.deepEqual(git("rev-list", `${baseline}..HEAD`).split("\n").filter(Boolean), [source]);
    tag = `v${nextVersion(latest.tag_name, ["fix(deps): update dependencies"])}`;
    kind = pull.labels.some((label: any) => label.name === "automation:security") ? "security" : "weekly";
  }
  appendFileSync(process.env.GITHUB_OUTPUT!, `tag=${tag}\ncommit=${source}\nkind=${kind}\nbase=${latest.tag_name}\n`);
}
