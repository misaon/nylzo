import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";
import batches from "./batches.cjs";

test("new pre-labelled migrations free proposal slots without loops or accepting another bot", () => {
  const root = mkdtempSync(join(tmpdir(), "nylzo-continuation-"));
  try {
    mkdirSync(join(root, "bin"));
    writeFileSync(join(root, "bin/gh"), `#!/usr/bin/env node
const fs=require('node:fs'),args=process.argv;
if(args.includes('POST')){fs.appendFileSync(process.env.WRITES,fs.readFileSync(0,'utf8')+'\\n');console.log('{}');}
else console.log(JSON.stringify([JSON.parse(fs.readFileSync(process.env.PULLS,'utf8'))]));
`); chmodSync(join(root, "bin/gh"), 0o700);
    const pull = { state: "open", user: { login: "nylzo-dependency-updates[bot]", id: 123456 }, base: { ref: "main" }, head: { repo: { full_name: "misaon/nylzo" } }, labels: [{ name: "requires-attention" }] };
    const run = (pulls: any[], previous: string, mode: string) => {
      writeFileSync(join(root, "pulls"), JSON.stringify(pulls)); writeFileSync(join(root, "writes"), "");
      const result = spawnSync(process.execPath, [resolve("tools/dependencies/continue.mts")], { encoding: "utf8", env: { ...process.env, PATH: `${root}/bin:${process.env.PATH}`, PULLS: join(root, "pulls"), WRITES: join(root, "writes"), PREVIOUS_BLOCKED: previous, NYLZO_UPDATE_MODE: mode, GITHUB_REPOSITORY: "misaon/nylzo", GITHUB_REF: "refs/heads/main", DEPENDENCY_APP_SLUG: "nylzo-dependency-updates", DEPENDENCY_BOT_ID: "123456" } });
      return { ...result, writes: readFileSync(join(root, "writes"), "utf8") };
    };
    const weekly = run([pull, pull, pull, pull], "0", "weekly"); assert.equal(weekly.status, 0, weekly.stderr);
    assert.deepEqual(JSON.parse(weekly.writes), { ref: "main", inputs: { mode: "weekly" } });
    const repeated = run([pull, pull, pull, pull], "4", "weekly"); assert.equal(repeated.status, 0); assert.equal(repeated.writes, "");
    const other = structuredClone(pull); other.user.id = 999;
    assert.equal(run([other], "0", "security").writes, "");
    const security = run([pull], "0", "security"); assert.equal(security.status, 0);
    assert.equal(JSON.parse(security.writes).inputs.mode, "security");
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test("only successful protected discovery with a free slot can record unavailable exact targets, without dispatch loops", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-unavailable-target-"));
  try {
    const repo = { full_name: "misaon/nylzo" }, bot = { login: "nylzo-dependency-updates[bot]", id: 123456 };
    const head = "b".repeat(40), base = "a".repeat(40), candidates = ["first", "second"].map(name => ({ name, manager: "npm", datasource: "npm", value: "1.2.4", version: "1.2.4" }));
    const batch = { base: "release/v0.1.6", phase: "batch", mode: "security", root: 0, head: "", candidates };
    const root = { number: 10, state: "open", user: bot, labels: [{ name: "requires-attention" }, { name: "automation:batch-failed" }, { name: "automation:ci-failed" }], head: { repo, sha: head, ref: "renovate/release/v0.1.6-security-dependency-batch" }, base: { repo, ref: batch.base } };
    const prefix = "repos/misaon/nylzo/";
    const routes: Record<string, any> = {
      [prefix + "pulls?state=open&per_page=100"]: [root],
      [prefix + "pulls/10/commits?per_page=100"]: [{ sha: head, author: bot, parents: [{ sha: base }], commit: { verification: { verified: true }, message: `fix(deps): test\n\nNylzo-Batch-V1: ${JSON.stringify(batch)}` } }],
      [prefix + "issues/10/comments?per_page=100"]: [],
      [prefix + `actions/workflows/ci.yml/runs?head_sha=${head}&event=pull_request&per_page=100`]: { workflow_runs: [{ id: 100, head_sha: head, head_repository: repo, event: "pull_request", status: "completed", conclusion: "failure" }] },
    };
    writeFileSync(join(directory, "gh"), `#!/usr/bin/env node
const fs=require('node:fs'),p=process.argv.find(v=>v.startsWith('repos/'));if(process.argv.includes('POST')){fs.appendFileSync(process.env.WRITES,JSON.stringify({path:p,body:JSON.parse(fs.readFileSync(0,'utf8'))})+'\\n');console.log('{}');}else{const r=JSON.parse(process.env.ROUTES);if(!(p in r))process.exit(1);console.log(JSON.stringify([r[p]]));}\n`); chmodSync(join(directory, "gh"), 0o700);
    const run = (result: string) => {
      writeFileSync(join(directory, "writes"), "");
      const output = spawnSync(process.execPath, [resolve("tools/dependencies/continue.mts")], { encoding: "utf8", env: { ...process.env, PATH: `${directory}:${process.env.PATH}`, ROUTES: JSON.stringify(routes), WRITES: join(directory, "writes"), PREVIOUS_BLOCKED: "1", NYLZO_UPDATE_MODE: "security", NYLZO_BATCH_PLAN: JSON.stringify({ plans: { [batch.base]: { phase: "diagnostic", root: 10, head, candidates } } }), RENOVATE_RESULT: result, GITHUB_REPOSITORY: "misaon/nylzo", GITHUB_REF: "refs/heads/main", DEPENDENCY_APP_SLUG: "nylzo-dependency-updates", DEPENDENCY_BOT_ID: "123456" } });
      return { ...output, writes: readFileSync(join(directory, "writes"), "utf8").trim().split("\n").filter(Boolean).map(line => JSON.parse(line)) };
    };
    assert.deepEqual(run("failure").writes, [], "A failed lookup cannot skip candidates");
    const complete = run("success"); assert.equal(complete.status, 0, complete.stderr);
    assert.equal(complete.writes.length, 2); assert.equal(complete.writes[0].path, prefix + "issues/10/comments");
    assert(complete.writes[0].body.body.includes(batches.unitId("first")));
    routes[prefix + "issues/10/comments?per_page=100"] = [{ user: { login: "github-actions[bot]", id: 41898282 }, body: complete.writes[0].body.body }];
    assert.deepEqual(run("success").writes, [], "The same unavailable-target record cannot cause a dispatch loop");
    routes[prefix + "issues/10/comments?per_page=100"] = [];
    for (const number of [11, 12]) routes[prefix + "pulls?state=open&per_page=100"].push({ ...root, number, labels: [], head: { ...root.head, ref: `renovate/other-${number}` } });
    assert.deepEqual(run("success").writes, [], "A full proposal budget means deferred, not unavailable");
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
