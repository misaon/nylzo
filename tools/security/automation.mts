import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { appendFileSync, readFileSync } from "node:fs";
import { api, inspectUpdate } from "./dependency-api.mts";
import { dependencyFiles, nextPatch, repository } from "./dependency-policy.mts";

const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH!, "utf8"));
assert.equal(process.env.GITHUB_REPOSITORY, repository);
assert.equal(process.env.GITHUB_REF, "refs/heads/main");
const run = api(`repos/${repository}/actions/runs/${event.workflow_run.id}`);
assert.equal(run.workflow_id, api(`repos/${repository}/actions/workflows/ci.yml`).id);
assert.equal(run.conclusion, "success");
assert.equal(run.head_repository.full_name, repository);
const jobs = api(`repos/${repository}/actions/runs/${run.id}/jobs?per_page=100`);
assert.equal(jobs.jobs.length, jobs.total_count);
const required = ["CI required", "Analyze (go)", "Analyze (javascript-typescript)", "Analyze (actions)"];
if (process.argv[2] === "release") required.push("Release verification");
for (const name of required) assert(jobs.jobs.some((job: any) => job.name === name && job.conclusion === "success"), `Missing successful ${name}`);
const git = (...arguments_: string[]) => execFileSync("git", arguments_, { encoding: "utf8" }).trim();

if (process.argv[2] === "merge") {
  assert.equal(run.event, "pull_request");
  const pulls = api(`repos/${repository}/commits/${run.head_sha}/pulls`);
  const pull = pulls.find((candidate: any) => candidate.head.sha === run.head_sha && candidate.state === "open" && candidate.user.id === 49699333);
  if (!pull) { console.log("No current Dependabot PR for this run."); process.exit(0); }
  let update;
  try { update = inspectUpdate(pull.number); } catch (error) {
    console.log(`Maintainer review required: ${(error as Error).message}`);
    process.exit(0);
  }
  assert.equal(update.head, run.head_sha);
  assert.equal(process.env.SECURITY_AUTOMATION_ENABLED, "true");
  const result = api(`repos/${repository}/pulls/${update.pull}/merge`, {
    sha: update.head, merge_method: "squash",
    commit_title: `fix(deps): update ${update.name} to ${update.next}`,
    commit_message: `Security update from Dependabot. Fixes ${update.fixed.join(", ")}.`,
  });
  assert.equal(result.merged, true);
  console.log(`Merged PR #${update.pull} at ${result.sha}; fixes ${update.fixed.join(", ")}.`);
} else {
  assert.equal(process.argv[2], "release");
  assert.equal(run.event, "push");
  assert.equal(run.head_branch, "main");
  assert.equal(process.env.GITHUB_SHA, run.head_sha);
  assert.equal(run.head_sha, api(`repos/${repository}/git/ref/heads/main`).object.sha);
  assert.equal(git("rev-parse", "HEAD"), run.head_sha);
  const latest = api(`repos/${repository}/releases/latest`);
  assert.equal(latest.draft, false);
  assert.equal(latest.prerelease, false);
  const tag = nextPatch(latest.tag_name);
  git("merge-base", "--is-ancestor", latest.tag_name, "HEAD");
  const commits = git("rev-list", `${latest.tag_name}..HEAD`).split("\n").filter(Boolean);
  if (commits.length === 0) { console.log("The checked commit is already released."); process.exit(0); }
  assert(commits.length <= 25, "Release backlog needs maintainer review");
  const paths = git("diff", "--name-only", `${latest.tag_name}..HEAD`).split("\n").filter(Boolean);
  assert(paths.every(path => dependencyFiles.has(path)), "Unreleased application or workflow changes need a maintainer release");
  for (const commit of commits) {
    const pulls = api(`repos/${repository}/commits/${commit}/pulls`);
    const pull = pulls.find((candidate: any) => candidate.merged_at && candidate.merge_commit_sha === commit);
    assert(pull, "Every unreleased commit must be an eligible merged security PR");
    const update = inspectUpdate(pull.number, true);
    assert.equal(update.mergedCommit, commit);
    console.log(`PR #${update.pull} fixes ${update.fixed.join(", ")}.`);
  }
  assert.equal(process.env.SECURITY_AUTOMATION_ENABLED, "true");
  appendFileSync(process.env.GITHUB_OUTPUT!, `tag=${tag}\ncommit=${run.head_sha}\n`);
}
