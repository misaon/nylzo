import assert from "node:assert/strict";
import { appendFileSync, readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { inspectDependencyPull, requireAttention, UpdateDeferred } from "../dependencies/inspect.mts";
import { contents, git, pages, repository, request } from "../release/github.mts";
import { nextVersion, releaseBranch, releaseMetadata, versionParts, trustedDependencyBot } from "../release/policy.mts";
import { changesAfter, lastMainRelease } from "../release/baseline.mts";
import batches from "../dependencies/batches.cjs";
import { batchRecords, verifyRegroup } from "../dependencies/batch-state.mts";

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

function continueUpdates(pull: any) {
  // The merger installation token remains Actions read-only.
  assert(process.env.WORKFLOW_DISPATCH_TOKEN, "A trusted dispatch token is required to continue the update queue");
  const mode = pull.labels.some((label: any) => label.name === "automation:weekly") ? "weekly" : "security";
  execFileSync("gh", ["api", `repos/${repository}/actions/workflows/dependencies.yml/dispatches`, "--method", "POST", "--input", "-"], {
    input: JSON.stringify({ ref: "main", inputs: { mode } }), stdio: ["pipe", "inherit", "inherit"],
    env: { ...process.env, GH_TOKEN: process.env.WORKFLOW_DISPATCH_TOKEN },
  });
}

function attention(pull: any, reason: string, kind: "ci" | "manual" = "manual") {
  const alreadyBlocked = pull.labels.some((label: any) => label.name === "requires-attention");
  requireAttention(pull, reason, kind);
  if (!alreadyBlocked) continueUpdates(pull);
}

function failedBatch(pull: any, batch: any, reason: string) {
  if (batch) request(`repos/${repository}/issues/${pull.number}/labels`, "POST", { labels: [batch.phase === "diagnostic" ? "automation:diagnostic-failed" : "automation:batch-failed"] });
  attention(pull, reason, "ci");
}

if (process.argv[2] === "merge") {
  assert.equal(run.event, "pull_request");
  const pulls = request(`repos/${repository}/commits/${run.head_sha}/pulls`);
  const pull = pulls.find((candidate: any) => candidate.head.sha === run.head_sha && candidate.state === "open" && trustedDependencyBot(candidate.user) && candidate.head.ref.startsWith("renovate/"));
  if (!pull) { console.log("No current automation dependency PR for this run."); process.exit(0); }
  let update;
  try { update = inspectDependencyPull(pull.number, false, latest); }
  catch (error) {
    if (error instanceof UpdateDeferred) console.log(error.message);
    else if (error instanceof Error && error.message === "Renovate reports an unresolved artifact generation failure") failedBatch(pull, batchRecords([pull])[0]?.batch, "Lockfile generation failed. Diagnose the signed batch without publishing individual probes.");
    else attention(pull, "The update changes code, requires a migration, or has unsafe versions or integrity metadata. Review the diff and CI.");
    process.exit(0);
  }
  if (update.batch?.phase === "diagnostic" || batches.diagnosticBranch(pull.head.ref)) {
    // A green diagnostic is evidence for a future combined PR, never a merge
    // authorization. Human holds remain terminal failures for isolation.
    const labels = new Set(pull.labels.map((label: any) => label.name));
    if (labels.has("manual-hold") || (labels.has("requires-attention") && !labels.has("automation:ci-failed"))) { continueUpdates(pull); process.exit(0); }
    if (run.conclusion === "success" && verified) {
      if (labels.has("automation:ci-failed")) request(`repos/${repository}/issues/${pull.number}/labels/automation%3Aci-failed`, "DELETE");
      if (labels.has("requires-attention")) request(`repos/${repository}/issues/${pull.number}/labels/requires-attention`, "DELETE");
      if (labels.has("automation:diagnostic-failed")) request(`repos/${repository}/issues/${pull.number}/labels/automation%3Adiagnostic-failed`, "DELETE");
      if (!labels.has("automation:diagnostic-passed") || labels.has("automation:ci-failed")) {
        request(`repos/${repository}/issues/${pull.number}/labels`, "POST", { labels: ["automation:diagnostic-passed"] });
        continueUpdates(pull);
      }
      console.log("Diagnostic passed; regroup the verified updates before any merge.");
    } else if (["failure", "timed_out", "action_required"].includes(run.conclusion)) failedBatch(pull, update.batch, "An isolated dependency failed verification. Keep it for maintainer review and regroup the verified remainder.");
    process.exit(0);
  }
  if (run.conclusion !== "success" || !verified) {
    if (["failure", "timed_out", "action_required"].includes(run.conclusion)) failedBatch(pull, update.batch, "CI or a security check failed. Diagnose the signed batch before regrouping; no merge or release attempted.");
    process.exit(0);
  }
  assert.equal(update.pull.head.sha, run.head_sha);
  if (update.batch?.phase === "regroup") verifyRegroup(update.batch, update.base);
  if (pull.base.ref !== "main") {
    const reference = request(`repos/${repository}/git/ref/tags/${latest.tag_name}`);
    const baseline = request(`repos/${repository}/git/tags/${reference.object.sha}`).object.sha;
    if (update.base !== baseline) { console.log("A verified patch is awaiting publication; do not stack another change onto it."); process.exit(0); }
  }
  // Re-read human holds after source and CI validation. A CI-only hold can
  // recover; a manual hold or an unclassified attention label stays blocked.
  const currentPull = request(`repos/${repository}/pulls/${pull.number}`);
  assert.equal(currentPull.head.sha, run.head_sha);
  if (currentPull.draft) process.exit(0);
  const labels = new Set(currentPull.labels.map((label: any) => label.name));
  if (labels.has("manual-hold") || (labels.has("requires-attention") && !labels.has("automation:ci-failed"))) {
    console.log("Maintainer attention label prevents automatic merge."); process.exit(0);
  }
  if (labels.has("automation:ci-failed")) {
    if (labels.has("requires-attention")) request(`repos/${repository}/issues/${pull.number}/labels/requires-attention`, "DELETE");
    request(`repos/${repository}/issues/${pull.number}/labels/automation%3Aci-failed`, "DELETE");
  }
  const result = request(`repos/${repository}/pulls/${pull.number}/merge`, "PUT", {
    sha: run.head_sha, merge_method: "squash", commit_title: pull.title,
    commit_message: `Verified dependency-only update from PR #${pull.number}. Full CI and CodeQL passed on ${run.head_sha}.`,
  });
  assert.equal(result.merged, true);
  if (pull.base.ref !== "main") {
    // Other proposals from the old published source can no longer merge.
    // Stop only their unchanged, signed App PR CI, never push/release runs.
    try {
      assert(process.env.WORKFLOW_DISPATCH_TOKEN);
      for (const candidate of pages(`repos/${repository}/pulls?state=open&per_page=100`)) {
        if (!trustedDependencyBot(candidate.user) || candidate.head.repo.full_name !== repository || candidate.base.ref !== pull.base.ref || !candidate.head.ref.startsWith("renovate/") || candidate.draft || candidate.labels.some((label: any) => label.name === "requires-attention" || label.name === "manual-hold")) continue;
        assert.match(candidate.head.sha, /^[a-f0-9]{40}$/u);
        const commit = request(`repos/${repository}/commits/${candidate.head.sha}`);
        if (!trustedDependencyBot(commit.author) || !commit.commit.verification.verified || commit.parents.length !== 1 || commit.parents[0].sha !== update.base) continue;
        const runs = pages(`repos/${repository}/actions/workflows/ci.yml/runs?head_sha=${candidate.head.sha}&event=pull_request&per_page=100`).flatMap(page => page.workflow_runs);
        for (const stale of runs) {
          if (stale.workflow_id !== run.workflow_id || stale.event !== "pull_request" || stale.head_repository.full_name !== repository || stale.head_sha !== candidate.head.sha || stale.head_branch !== candidate.head.ref || stale.status === "completed") continue;
          assert(Number.isSafeInteger(stale.id));
          try {
            execFileSync("gh", ["api", `repos/${repository}/actions/runs/${stale.id}/cancel`, "--method", "POST"], {
              stdio: ["ignore", "inherit", "inherit"], env: { ...process.env, GH_TOKEN: process.env.WORKFLOW_DISPATCH_TOKEN },
            });
            console.log(`Cancelled obsolete proposal CI ${stale.id}; PR #${candidate.number} needs a fresh base and full CI.`);
          } catch (error) { console.warn(`Could not cancel obsolete CI ${stale.id}:`, error); }
        }
        request(`repos/${repository}/pulls/${candidate.number}`, "PATCH", { state: "closed" });
        console.log(`Closed superseded machine proposal #${candidate.number}; discovery recreates it on the next published baseline.`);
      }
    } catch (error) { console.warn("Merge succeeded; obsolete proposal CI cleanup needs checking:", error); }
  } else continueUpdates(pull);
  console.log(`Merged verified dependency PR #${pull.number}. Ordinary main commits do not trigger deployment.`);
} else {
  assert.equal(process.argv[2], "release");
  assert.equal(run.event, "push"); assert.equal(run.conclusion, "success"); assert(verified);
  assert(allJobs.some(job => job.name === "Release verification" && job.conclusion === "success"));
  const source = run.head_sha;
  assert.match(source, /^[a-f0-9]{40}$/u);
  const current = request(`repos/${repository}/git/ref/heads/${run.head_branch}`).object.sha;
  if (run.head_branch === "main" && current !== source) {
    const comparison = request(`repos/${repository}/compare/${source}...${current}`);
    assert.equal(comparison.status, "ahead", "Approved release source must remain an ancestor of protected main");
  } else assert.equal(current, source, "Maintenance ref changed after CI");
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
    const expected = `v${nextVersion(latest.tag_name, messages)}`;
    if (tag !== expected) {
      requireAttention(detail, "An intervening published patch changed the next version. Review the regenerated release PR before publication.");
      if (detail.labels.some((label: any) => label.name === "autorelease: pending")) request(`repos/${repository}/issues/${pull.number}/labels/${encodeURIComponent("autorelease: pending")}`, "DELETE");
      assert(process.env.WORKFLOW_DISPATCH_TOKEN);
      execFileSync("gh", ["api", `repos/${repository}/actions/workflows/release-please.yml/dispatches`, "--method", "POST", "--input", "-"], {
        input: JSON.stringify({ ref: "main" }), env: { ...process.env, GH_TOKEN: process.env.WORKFLOW_DISPATCH_TOKEN },
      });
      console.log(`Approved metadata is stale; a new release PR must authorize ${expected}. No tag created.`);
      process.exit(0);
    }
    kind = "planned";
  } else {
    assert.equal(run.head_branch, releaseBranch(latest.tag_name));
    const update = inspectDependencyPull(pull.number, true, latest);
    assert(update.batch?.phase !== "diagnostic" && !batches.diagnosticBranch(pull.head.ref), "Diagnostic commits can never authorize publication");
    const baseline = git("rev-parse", `${latest.tag_name}^{commit}`);
    assert.equal(update.base, baseline, "Automatic patch must contain exactly one verified update from the last published source");
    assert.deepEqual(git("rev-list", `${baseline}..HEAD`).split("\n").filter(Boolean), [source]);
    tag = `v${nextVersion(latest.tag_name, ["fix(deps): update dependencies"])}`;
    kind = pull.labels.some((label: any) => label.name === "automation:security") ? "security" : "weekly";
  }
  appendFileSync(process.env.GITHUB_OUTPUT!, `tag=${tag}\ncommit=${source}\nkind=${kind}\nbase=${latest.tag_name}\n`);
}
