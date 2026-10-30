import assert from "node:assert/strict";
import batches from "./batches.cjs";
import { pages, repository } from "../release/github.mts";
import { trustedDependencyBot } from "../release/policy.mts";

const required = ["Release verification", "CI required", "Analyze (go)", "Analyze (javascript-typescript)", "Analyze (actions)"];

export function batchRecords(pulls: any[]) {
  const records: any[] = [];
  for (const pull of pulls) {
    if (!trustedDependencyBot(pull.user) || pull.head.repo.full_name !== repository || !/^renovate\//u.test(pull.head.ref) || !/-(?:dependency-batch|diagnose-\d+-[a-f0-9]{12}|regroup-\d+-[a-f0-9]{12})$/u.test(pull.head.ref)) continue;
    const commits = pages(`repos/${repository}/pulls/${pull.number}/commits?per_page=100`);
    if (commits.length !== 1 || commits[0].sha !== pull.head.sha || !trustedDependencyBot(commits[0].author) || !commits[0].commit.verification.verified || commits[0].parents.length !== 1) continue;
    let batch;
    try { batch = batches.readBatch(commits[0].commit.message ?? ""); } catch { continue; }
    if (!batch || batch.base !== pull.base.ref) continue;
    const labels = pull.labels.map((label: any) => label.name);
    if (pull.draft) labels.push("manual-hold");
    const comments = pages(`repos/${repository}/issues/${pull.number}/comments?per_page=100`);
    const artifactFailed = comments.some(comment => trustedDependencyBot(comment.user) && /^### ⚠️ Artifact update problem(?:\r?\n|$)/u.test(comment.body ?? ""));
    const record = { number: pull.number, head: pull.head.sha, ref: pull.head.ref, base: commits[0].parents[0].sha, labels, batch, artifactFailed, terminal: undefined as undefined | "pass" | "fail", unavailable: [] as string[] };
    if (batch.phase === "batch" && labels.includes("automation:batch-failed")) {
      for (const comment of comments) {
        if (comment.user?.login !== "github-actions[bot]" || comment.user?.id !== 41898282) continue;
        const match = /^<!-- nylzo-batch-unavailable-v1 (.{1,8192}) -->\n/u.exec(comment.body ?? "");
        if (!match) continue;
        try {
          const data = JSON.parse(match[1]!);
          if (data.head === record.head && Array.isArray(data.units) && data.units.every((id: any) => typeof id === "string" && batches.units(batch.candidates).has(id))) record.unavailable.push(...data.units);
        } catch { /* An unrelated or malformed comment is not evidence. */ }
      }
    }
    if (labels.includes("manual-hold") || artifactFailed) record.terminal = "fail";
    else if (labels.includes("automation:ci-failed") || (batch.phase === "diagnostic" && labels.includes("automation:diagnostic-passed"))) {
      // Labels alone are not evidence: recheck the exact signed head and every
      // required analysis before including a diagnostic result in a regroup.
      const runs = pages(`repos/${repository}/actions/workflows/ci.yml/runs?head_sha=${pull.head.sha}&event=pull_request&per_page=100`).flatMap(page => page.workflow_runs);
      const run = runs.filter(item => item.head_sha === pull.head.sha && item.head_repository.full_name === repository && item.event === "pull_request").sort((a, b) => b.id - a.id)[0];
      if (run?.status === "completed" && ["failure", "timed_out", "action_required"].includes(run.conclusion)) record.terminal = "fail";
      if (run?.status === "completed" && run.conclusion === "success") {
        const jobs = pages(`repos/${repository}/actions/runs/${run.id}/jobs?per_page=100`).flatMap(page => page.jobs);
        if (batch.phase === "diagnostic" && labels.includes("automation:diagnostic-passed") && required.every(name => jobs.some(job => job.name === name && job.conclusion === "success"))) record.terminal = "pass";
      }
    }
    records.push(record);
  }
  assert(records.length <= 1000);
  return records;
}

export function discoveryPlan(pulls: any[], releaseBranch: string, source: string, main: string, mode: string) {
  const records = batchRecords(pulls), plans: Record<string, any> = {}, excluded: any[] = [];
  for (const [branch, sha] of [[releaseBranch, source], ["main", main]]) {
    const result = batches.planIsolation(records, branch, sha, mode);
    excluded.push(...result.excluded);
    if (result.plan) plans[branch] = result.plan;
  }
  return { plans, excluded: [...new Map(excluded.map(item => [batches.key(item), item])).values()] };
}

export function verifyRegroup(batch: any, base: string) {
  assert.equal(batch.phase, "regroup");
  const records = batchRecords(pages(`repos/${repository}/pulls?state=open&per_page=100`));
  const expected = batches.planIsolation(records, batch.base, base, batch.mode).plan;
  assert(expected?.phase === "regroup" && expected.root === batch.root && expected.head === batch.head && batches.same(expected.candidates, batch.candidates), "Recombined updates require the exact independently verified diagnostic plan");
}
