import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";
import batches from "./batches.cjs";

test("diagnostic labels require the exact signed head, current CI and all analyses before recombination", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-batch-state-"));
  try {
    const bot = { login: "nylzo-dependency-updates[bot]", id: 123456 }, repo = { full_name: "misaon/nylzo" };
    const source = "a".repeat(40), rootHead = "b".repeat(40), diagHead = "c".repeat(40);
    const first = { name: "first", manager: "npm", datasource: "npm", value: "1.2.4", version: "1.2.4" }, second = { ...first, name: "second" };
    const rootBatch = { base: "release/v0.1.6", phase: "batch", mode: "security", root: 0, head: "", candidates: [first, second] };
    const diagBatch = { ...rootBatch, phase: "diagnostic", root: 10, head: rootHead, candidates: [first] };
    const root = { number: 10, user: bot, labels: [{ name: "automation:batch-failed" }, { name: "automation:ci-failed" }], head: { repo, sha: rootHead, ref: "renovate/release/v0.1.6-security-dependency-batch" }, base: { repo, ref: "release/v0.1.6" } };
    const diagnostic = { ...root, number: 11, labels: [{ name: "automation:diagnostic-passed" }], head: { repo, sha: diagHead, ref: `renovate/release/v0.1.6-diagnose-10-${batches.unitId("first")}` } };
    const commit = (sha: string, batch: any) => ({ sha, author: bot, parents: [{ sha: source }], commit: { verification: { verified: true }, message: `fix(deps): test\n\nNylzo-Batch-V1: ${JSON.stringify(batch)}` } });
    const required = ["Release verification", "CI required", "Analyze (go)", "Analyze (javascript-typescript)", "Analyze (actions)"];
    const prefix = "repos/misaon/nylzo/";
    const routes: Record<string, any> = {
      [prefix + "pulls?state=open&per_page=100"]: [root, diagnostic],
      [prefix + "pulls/10/commits?per_page=100"]: [commit(rootHead, rootBatch)],
      [prefix + "pulls/11/commits?per_page=100"]: [commit(diagHead, diagBatch)],
      [prefix + "issues/10/comments?per_page=100"]: [],
      [prefix + "issues/11/comments?per_page=100"]: [],
      [prefix + `actions/workflows/ci.yml/runs?head_sha=${rootHead}&event=pull_request&per_page=100`]: { workflow_runs: [{ id: 100, status: "completed", conclusion: "failure", head_sha: rootHead, event: "pull_request", head_repository: repo }] },
      [prefix + `actions/workflows/ci.yml/runs?head_sha=${diagHead}&event=pull_request&per_page=100`]: { workflow_runs: [{ id: 101, status: "completed", conclusion: "success", head_sha: diagHead, event: "pull_request", head_repository: repo }] },
      [prefix + "actions/runs/101/jobs?per_page=100"]: { jobs: required.map(name => ({ name, conclusion: "success" })) },
    };
    writeFileSync(join(directory, "gh"), `#!/usr/bin/env node
const r=JSON.parse(process.env.ROUTES),p=process.argv.find(v=>v.startsWith('repos/'));if(!(p in r))process.exit(1);console.log(JSON.stringify([r[p]]));\n`); chmodSync(join(directory, "gh"), 0o700);
    const run = (regroup?: any) => spawnSync(process.execPath, ["--input-type=module", "--eval", `import {batchRecords,verifyRegroup} from ${JSON.stringify(pathToFileURL(resolve("tools/dependencies/batch-state.mts")).href)}; if(process.env.REGROUP) { verifyRegroup(JSON.parse(process.env.REGROUP), ${JSON.stringify(source)}); console.log('{}'); } else console.log(JSON.stringify(batchRecords(JSON.parse(process.env.PULLS))));`], { encoding: "utf8", env: { ...process.env, PATH: `${directory}:${process.env.PATH}`, ROUTES: JSON.stringify(routes), PULLS: JSON.stringify([root, diagnostic]), ...(regroup ? { REGROUP: JSON.stringify(regroup) } : {}), DEPENDENCY_APP_SLUG: "nylzo-dependency-updates", DEPENDENCY_BOT_ID: "123456" } });
    let result = run(); assert.equal(result.status, 0, result.stderr); assert.equal(JSON.parse(result.stdout)[1].terminal, "pass");
    const marker = { head: rootHead, units: [batches.unitId("second")] };
    const comment = { user: { login: "github-actions[bot]", id: 41898282 }, body: `<!-- nylzo-batch-unavailable-v1 ${JSON.stringify(marker)} -->\nUnavailable exact target.` };
    routes[prefix + "issues/10/comments?per_page=100"] = [comment];
    result = run(); assert.deepEqual(JSON.parse(result.stdout)[0].unavailable, marker.units);
    comment.user.id = 42;
    result = run(); assert.deepEqual(JSON.parse(result.stdout)[0].unavailable, [], "Another account cannot forge unavailable-target evidence");
    routes[prefix + "actions/runs/101/jobs?per_page=100"].jobs.pop();
    result = run(); assert.equal(result.status, 0, result.stderr); assert.equal(JSON.parse(result.stdout)[1].terminal, undefined);
    routes[prefix + `actions/workflows/ci.yml/runs?head_sha=${diagHead}&event=pull_request&per_page=100`].workflow_runs[0].head_sha = "d".repeat(40);
    result = run(); assert.equal(JSON.parse(result.stdout)[1].terminal, undefined);
    routes[prefix + "pulls/11/commits?per_page=100"][0].commit.verification.verified = false;
    result = run(); assert.equal(JSON.parse(result.stdout).length, 1, "A label cannot replace the App signature");
    routes[prefix + "pulls/11/commits?per_page=100"][0].commit.verification.verified = true;
    routes[prefix + "actions/runs/101/jobs?per_page=100"].jobs.push({ name: required.at(-1), conclusion: "success" });
    routes[prefix + `actions/workflows/ci.yml/runs?head_sha=${diagHead}&event=pull_request&per_page=100`].workflow_runs[0].head_sha = diagHead;
    const failedHead = "d".repeat(40), failed = { ...diagnostic, number: 12, labels: [{ name: "manual-hold" }], head: { repo, sha: failedHead, ref: `renovate/release/v0.1.6-diagnose-10-${batches.unitId("second")}` } };
    routes[prefix + "pulls?state=open&per_page=100"].push(failed);
    routes[prefix + "pulls/12/commits?per_page=100"] = [commit(failedHead, { ...diagBatch, candidates: [second] })];
    routes[prefix + "issues/12/comments?per_page=100"] = [];
    const regroup = { ...diagBatch, phase: "regroup" };
    result = run(regroup); assert.equal(result.status, 0, result.stderr);
    assert.notEqual(run({ ...regroup, candidates: [second] }).status, 0, "A failed unit cannot be smuggled into a recombination");
    routes[prefix + `actions/workflows/ci.yml/runs?head_sha=${diagHead}&event=pull_request&per_page=100`].workflow_runs[0].status = "in_progress";
    assert.notEqual(run(regroup).status, 0, "A new incomplete CI attempt invalidates the old diagnostic approval");
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
