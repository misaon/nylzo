import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { contents, pages, repository, request } from "./github.mts";
import { releaseMetadata, trustedDependencyBot } from "./policy.mts";

assert.equal(process.env.GITHUB_REPOSITORY, repository);
assert.equal(process.env.GITHUB_REF, "refs/heads/main");
const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH!, "utf8"));
const run = request(`repos/${repository}/actions/runs/${event.workflow_run.id}`);
assert.equal(run.workflow_id, request(`repos/${repository}/actions/workflows/release.yml`).id);
assert.equal(run.head_repository.full_name, repository);
assert.equal(run.event, "push");
assert.match(run.head_sha, /^[a-f0-9]{40}$/u);
const pulls = request(`repos/${repository}/commits/${run.head_sha}/pulls`);
for (const pull of pulls.filter((candidate: any) => candidate.merged_at && candidate.merge_commit_sha === run.head_sha && trustedDependencyBot(candidate.user) && candidate.head.repo.full_name === repository && (candidate.head.ref === "release-please--branches--main" || candidate.head.ref.startsWith("renovate/")))) {
  const detail = request(`repos/${repository}/pulls/${pull.number}`);
  const planned = pull.head.ref === "release-please--branches--main";
  const tag = planned ? releaseMetadata(detail, pages(`repos/${repository}/pulls/${pull.number}/files?per_page=100`), contents(".release-please-manifest.json", run.head_sha)) : run.head_branch;
  if (run.conclusion === "success") {
    const release = request(`repos/${repository}/releases/tags/${tag}`);
    assert.equal(release.immutable, true); assert.equal(release.draft, false); assert.equal(release.prerelease, false);
    if (planned) request(`repos/${repository}/issues/${pull.number}/labels`, "POST", { labels: ["autorelease: tagged"] });
  } else {
    request(`repos/${repository}/issues/${pull.number}/labels`, "POST", { labels: ["requires-attention"] });
    request(`repos/${repository}/issues/${pull.number}/assignees`, "POST", { assignees: ["misaon"] });
  }
  if (detail.labels.some((label: any) => label.name === "autorelease: pending")) request(`repos/${repository}/issues/${pull.number}/labels/${encodeURIComponent("autorelease: pending")}`, "DELETE");
}
if (run.conclusion === "success") {
  // Drain the remaining safe updates on the new published baseline instead of
  // leaving the rest of a weekly batch waiting for the following week's cron.
  assert.match(run.head_branch, /^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$/u);
  const release = request(`repos/${repository}/releases/tags/${run.head_branch}`);
  assert.equal(release.immutable, true); assert.equal(release.draft, false); assert.equal(release.prerelease, false);
  const reference = request(`repos/${repository}/git/ref/tags/${run.head_branch}`);
  assert.equal(reference.object.type, "tag");
  const tag = request(`repos/${repository}/git/tags/${reference.object.sha}`);
  assert.equal(tag.object.type, "commit"); assert.equal(tag.object.sha, run.head_sha);
  const kinds = [...tag.message.matchAll(/^Release-Kind: (planned|weekly|security)$/gmu)];
  assert(kinds.length <= 1);
  const mode = kinds[0]?.[1] === "weekly" ? "weekly" : "security";
  request(`repos/${repository}/actions/workflows/release-please.yml/dispatches`, "POST", { ref: "main" });
  request(`repos/${repository}/actions/workflows/dependencies.yml/dispatches`, "POST", { ref: "main", inputs: { mode } });
  console.log("Published release recorded; planned release PR refresh dispatched.");
}
