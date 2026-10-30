import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";
import batches from "../dependencies/batches.cjs";

const automation = resolve("tools/security/automation.mts");
function fixture(extraFiles: Record<string, [string, string]> = {}) {
  const root = mkdtempSync(join(tmpdir(), "nylzo-flow-"));
  const git = (...args: string[]) => execFileSync("git", ["-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "core.hooksPath=/dev/null", ...args], { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  git("init", "--initial-branch=main"); git("config", "user.email", "fixture@example.invalid"); git("config", "user.name", "Fixture");
  mkdirSync(join(root, "web/app"), { recursive: true });
  const before = '{"dependencies":{"example":"1.2.3"}}', after = before.replace("1.2.3", "1.2.4");
  writeFileSync(join(root, "web/app/package.json"), before);
  for (const [path, [content]] of Object.entries(extraFiles)) writeFileSync(join(root, path), content);
  git("add", "."); git("commit", "-m", "fix: released baseline");
  const baseline = git("rev-parse", "HEAD"); git("tag", "v0.1.6");
  writeFileSync(join(root, "unreleased-feature"), "must remain outside production"); git("add", "."); git("commit", "-m", "feat: unreleased feature");
  const main = git("rev-parse", "HEAD");
  git("switch", "--create", "release/v0.1.6", baseline);
  writeFileSync(join(root, "web/app/package.json"), after);
  for (const [path, [, content]] of Object.entries(extraFiles)) writeFileSync(join(root, path), content);
  git("add", "."); git("commit", "-m", "fix(deps): update dependency batch");
  const source = git("rev-parse", "HEAD"), bot = { login: "nylzo-dependency-updates[bot]", id: 123456 };
  const repo = { full_name: "misaon/nylzo" };
  const pull = { number: 7, merged: true, merged_at: "2026-10-29T00:00:00Z", merge_commit_sha: source,
    state: "closed", draft: false, title: "fix(deps): update dependency batch", user: bot, labels: [], changed_files: 1 + Object.keys(extraFiles).length,
    head: { sha: source, ref: "renovate/example", repo }, base: { ref: "release/v0.1.6", sha: baseline, repo } };
  const run = { id: 123, workflow_id: 12, event: "push", head_branch: "release/v0.1.6", head_sha: source, head_repository: repo, conclusion: "success" };
  const prefix = "repos/misaon/nylzo/";
  const routes: Record<string, any> = {
    [prefix + "actions/runs/123"]: run,
    [prefix + "actions/workflows/ci.yml"]: { id: 12 },
    [prefix + "actions/runs/123/jobs?per_page=100"]: { jobs: ["Release verification", "CI required", "Analyze (go)", "Analyze (javascript-typescript)", "Analyze (actions)"].map(name => ({ name, conclusion: "success" })) },
    [prefix + "releases/latest"]: { tag_name: "v0.1.6", immutable: true, draft: false, prerelease: false },
    [prefix + "git/ref/heads/release/v0.1.6"]: { object: { sha: source } },
    [prefix + `commits/${source}/pulls`]: [pull],
    [prefix + "pulls/7"]: pull,
    [prefix + "issues/7/comments?per_page=100"]: [],
    [prefix + "pulls?state=open&per_page=100"]: [],
    [prefix + "pulls/7/commits?per_page=100"]: [{ sha: source, author: bot, parents: [{ sha: baseline }], commit: { verification: { verified: true } } }],
    [prefix + "pulls/7/files?per_page=100"]: [{ filename: "web/app/package.json", status: "modified" }],
    [prefix + `contents/web/app/package.json?ref=${baseline}`]: { type: "file", encoding: "base64", content: Buffer.from(before).toString("base64") },
    [prefix + `contents/tools/dependencies/assets.json?ref=${baseline}`]: { type: "file", encoding: "base64", content: readFileSync("tools/dependencies/assets.json").toString("base64") },
    [prefix + `contents/web/app/package.json?ref=${source}`]: { type: "file", encoding: "base64", content: Buffer.from(after).toString("base64") },
  };
  for (const [path, [previous, next]] of Object.entries(extraFiles)) {
    routes[prefix + "pulls/7/files?per_page=100"].push({ filename: path, status: "modified" });
    for (const [ref, content] of [[baseline, previous], [source, next]]) {
      routes[prefix + `contents/${path}?ref=${ref}`] = { type: "file", encoding: "base64", content: Buffer.from(content!).toString("base64") };
    }
  }
  mkdirSync(join(root, "bin"));
  writeFileSync(join(root, "bin/gh"), `#!/usr/bin/env node
const fs=require('node:fs'),routes=JSON.parse(fs.readFileSync(process.env.ROUTES,'utf8'));
const args=process.argv.slice(2),path=args.find(value=>value.startsWith('repos/'));
const method=args.includes('--method')?args[args.indexOf('--method')+1]:'GET';
if(method!=='GET'){const body=args.includes('--input')?JSON.parse(fs.readFileSync(0,'utf8')):{};fs.appendFileSync(process.env.WRITES,JSON.stringify({path,method,body,dispatchToken:process.env.GH_TOKEN==='test-only'})+'\\n');console.log(JSON.stringify(path.endsWith('/merge')?{merged:true}:{}));process.exit(0);}
if(!(path in routes)){console.error('Unexpected fixture route');process.exit(1);}
console.log(JSON.stringify(args.includes('--slurp')?[routes[path]]:routes[path]));
`); chmodSync(join(root, "bin/gh"), 0o700);
  const execute = (command = "release", executable = automation) => {
    writeFileSync(join(root, "routes.json"), JSON.stringify(routes));
    writeFileSync(join(root, "event.json"), JSON.stringify({ workflow_run: { id: 123 } }));
    writeFileSync(join(root, "output"), ""); writeFileSync(join(root, "writes"), "");
    const result = spawnSync(process.execPath, [executable, command], { cwd: root, encoding: "utf8", timeout: 30000,
      env: { ...process.env, PATH: `${root}/bin:${process.env.PATH}`, ROUTES: join(root, "routes.json"), WRITES: join(root, "writes"),
        GITHUB_EVENT_PATH: join(root, "event.json"), GITHUB_OUTPUT: join(root, "output"), GITHUB_REPOSITORY: "misaon/nylzo", GITHUB_REF: "refs/heads/main",
        GH_TOKEN: "fixture-merger-token", WORKFLOW_DISPATCH_TOKEN: "test-only", GITHUB_SHA: main, SECURITY_AUTOMATION_ENABLED: "true", DEPENDENCY_APP_SLUG: "nylzo-dependency-updates", DEPENDENCY_BOT_ID: "123456" } });
    return { ...result, output: readFileSync(join(root, "output"), "utf8"), writes: readFileSync(join(root, "writes"), "utf8") };
  };
  return { root, git, main, baseline, source, pull, run, routes, prefix, execute, cleanup: () => rmSync(root, { recursive: true, force: true }) };
}

test("automatic patch publication excludes a feature already merged into main", () => {
  const f = fixture();
  try {
    const result = f.execute(); assert.equal(result.status, 0, result.stderr);
    assert(result.output.includes("tag=v0.1.7\n")); assert(result.output.includes(`commit=${f.source}\n`));
    assert.notEqual(f.source, f.main);
    assert.equal(f.git("diff", "--name-only", "v0.1.6..HEAD"), "web/app/package.json");
    assert.equal(result.writes, "", "Tag candidate validation itself performs no writes");
    f.routes[f.prefix + "pulls/7/files?per_page=100"].push({ filename: "internal/server/server.go", status: "modified" });
    f.pull.changed_files = 2;
    const unsafe = f.execute(); assert.notEqual(unsafe.status, 0); assert.equal(unsafe.output, "");
  } finally { f.cleanup(); }
});

test("one signed batch across npm, Go and tooling produces one patch candidate and fails closed as a whole", () => {
  const f = fixture({
    "go.mod": ["module example.invalid/batch\n\nrequire example.invalid/module v1.2.3\n", "module example.invalid/batch\n\nrequire example.invalid/module v1.2.4\n"],
    "mise.toml": ['[tools]\nactionlint = "1.7.11"\n', '[tools]\nactionlint = "1.7.12"\n'],
  });
  try {
    const candidate = f.execute(); assert.equal(candidate.status, 0, candidate.stderr);
    assert.equal(candidate.output.match(/^tag=/gmu)?.length, 1);
    assert(candidate.output.includes("tag=v0.1.7\n")); assert(candidate.output.includes(`commit=${f.source}\n`));
    assert.equal(f.git("rev-list", "--count", `${f.baseline}..${f.source}`), "1");
    assert.equal(f.git("diff", "--name-only", `${f.baseline}..${f.source}`), "go.mod\nmise.toml\nweb/app/package.json");
    assert.equal(candidate.writes, "");
    f.routes[f.prefix + "actions/runs/123/jobs?per_page=100"].jobs[0].conclusion = "failure";
    const failedCI = f.execute(); assert.notEqual(failedCI.status, 0); assert.equal(failedCI.output, ""); assert.equal(failedCI.writes, "");
    f.routes[f.prefix + "actions/runs/123/jobs?per_page=100"].jobs[0].conclusion = "success";
    const path = f.prefix + `contents/go.mod?ref=${f.source}`;
    f.routes[path].content = Buffer.from("module example.invalid/batch\n\nrequire example.invalid/module v2.0.0\n").toString("base64");
    const breakingMember = f.execute(); assert.notEqual(breakingMember.status, 0); assert.equal(breakingMember.output, ""); assert.equal(breakingMember.writes, "");
  } finally { f.cleanup(); }
});

function diagnosticFixture() {
  const f = fixture();
  const batch = { base: "release/v0.1.6", phase: "diagnostic", mode: "security", root: 10, head: "c".repeat(40), candidates: [{ name: "example", manager: "npm", datasource: "npm", value: "1.2.4", version: "1.2.4" }] };
  f.pull.head.ref = `renovate/release/v0.1.6-diagnose-10-${batches.unitId("example")}`;
  f.routes[f.prefix + "pulls/7/commits?per_page=100"][0].commit.message = `fix(deps): diagnose example\n\nNylzo-Batch-V1: ${JSON.stringify(batch)}`;
  return f;
}

test("a fully green diagnostic records evidence and continues discovery without merging or tagging", () => {
  const f = diagnosticFixture();
  try {
    f.run.event = "pull_request"; f.pull.merged = false; f.pull.state = "open";
    f.routes[f.prefix + "git/ref/heads/release/v0.1.6"] = { object: { sha: f.baseline } };
    const result = f.execute("merge"); assert.equal(result.status, 0, result.stderr);
    const writes = result.writes.trim().split("\n").map(line => JSON.parse(line));
    assert.deepEqual(writes.map(write => write.body), [{ labels: ["automation:diagnostic-passed"] }, { ref: "main", inputs: { mode: "security" } }]);
    assert(!writes.some(write => write.method === "PUT")); assert.equal(result.output, "");
    f.run.conclusion = "failure";
    const failed = f.execute("merge"); assert.equal(failed.status, 0, failed.stderr);
    assert(failed.writes.includes("automation:diagnostic-failed")); assert(failed.writes.includes("requires-attention"));
    assert(!failed.writes.includes('"merge_method"'));
  } finally { f.cleanup(); }
});

test("even a manually merged diagnostic cannot authorize a production release", () => {
  const f = diagnosticFixture();
  try {
    const result = f.execute(); assert.notEqual(result.status, 0);
    assert(result.stderr.includes("Diagnostic commits can never authorize publication"));
    assert.equal(result.output, ""); assert.equal(result.writes, "");
  } finally { f.cleanup(); }
});

test("failed post-merge CI retries all jobs within a fixed budget and never retries a stale source", () => {
  const f = fixture();
  try {
    Object.assign(f.run, { status: "completed", conclusion: "failure", run_attempt: 1 });
    const tagSha = "d".repeat(40);
    f.routes[f.prefix + "git/ref/tags/v0.1.6"] = { object: { type: "tag", sha: tagSha } };
    f.routes[f.prefix + `git/tags/${tagSha}`] = { object: { type: "commit", sha: f.baseline } };
    const run = () => f.execute("", resolve("tools/dependencies/retry-ci.mts"));
    for (const attempt of [1, 2]) {
      f.run.run_attempt = attempt;
      const retry = run(); assert.equal(retry.status, 0, retry.stderr);
      const writes = retry.writes.trim().split("\n").map(line => JSON.parse(line));
      assert.equal(writes.length, 1); assert.equal(writes[0].path, f.prefix + "actions/runs/123/rerun");
      assert(!writes[0].path.includes("rerun-failed-jobs"));
    }
    f.run.run_attempt = 3;
    const exhausted = run(); assert.equal(exhausted.status, 0, exhausted.stderr);
    assert.deepEqual(exhausted.writes.trim().split("\n").map(line => JSON.parse(line).body), [{ labels: ["requires-attention"] }, { assignees: ["misaon"] }]);
    f.run.run_attempt = 1; f.run.status = "in_progress";
    assert.equal(run().writes, "");
    f.run.status = "completed";
    f.routes[f.prefix + "git/ref/heads/release/v0.1.6"] = { object: { sha: "f".repeat(40) } };
    assert.equal(run().writes, "");
  } finally { f.cleanup(); }
});

test("cancelled maintenance CI retries only a GitHub-confirmed timeout from the current attempt", () => {
  const f = fixture();
  try {
    Object.assign(f.run, { status: "completed", conclusion: "cancelled", run_attempt: 2 });
    const tagSha = "d".repeat(40);
    f.routes[f.prefix + "git/ref/tags/v0.1.6"] = { object: { type: "tag", sha: tagSha } };
    f.routes[f.prefix + `git/tags/${tagSha}`] = { object: { type: "commit", sha: f.baseline } };
    const job = { conclusion: "cancelled", check_run_url: "https://api.github.com/repos/misaon/nylzo/check-runs/456" };
    f.routes[f.prefix + "actions/runs/123/attempts/2/jobs?per_page=100"] = { jobs: [job] };
    const annotation = { annotation_level: "failure", message: "The job has exceeded the maximum execution time of 20m0s" };
    f.routes[f.prefix + "check-runs/456/annotations?per_page=100"] = [];
    const run = () => f.execute("", resolve("tools/dependencies/retry-ci.mts"));
    const manual = run(); assert.equal(manual.status, 0, manual.stderr); assert.equal(manual.writes, "");
    f.routes[f.prefix + "check-runs/456/annotations?per_page=100"] = [annotation];
    const timeout = run(); assert.equal(timeout.status, 0, timeout.stderr);
    assert.deepEqual(timeout.writes.trim().split("\n").map(line => JSON.parse(line).path), [f.prefix + "actions/runs/123/rerun"]);
    annotation.annotation_level = "warning";
    assert.equal(run().writes, "");
    annotation.annotation_level = "failure"; annotation.message = "The operation was canceled.";
    assert.equal(run().writes, "");
    annotation.message = "The job has exceeded the maximum execution time of 20m0s";
    f.run.run_attempt = 3;
    f.routes[f.prefix + "actions/runs/123/attempts/3/jobs?per_page=100"] = { jobs: [job] };
    const exhausted = run(); assert.equal(exhausted.status, 0, exhausted.stderr);
    assert.deepEqual(exhausted.writes.trim().split("\n").map(line => JSON.parse(line).body), [{ labels: ["requires-attention"] }, { assignees: ["misaon"] }]);
    f.run.run_attempt = 2;
    job.check_run_url = "https://example.invalid/repos/misaon/nylzo/check-runs/456";
    const foreign = run(); assert.notEqual(foreign.status, 0); assert.equal(foreign.writes, "");
  } finally { f.cleanup(); }
});

test("the maintenance watchdog detects an unpublished stalled source without exposing host credentials", () => {
  const f = fixture();
  try {
    const tagSha = "d".repeat(40);
    f.routes[f.prefix + "git/ref/tags/v0.1.6"] = { object: { type: "tag", sha: tagSha } };
    f.routes[f.prefix + `git/tags/${tagSha}`] = { object: { type: "commit", sha: f.baseline } };
    const branch = { ref: "refs/heads/release/v0.1.6", object: { sha: f.source } };
    f.routes[f.prefix + "git/matching-refs/heads/release/v0.1.6"] = [branch];
    const commit = { commit: { committer: { date: new Date(Date.now() - 30 * 60 * 1000).toISOString() } } };
    f.routes[f.prefix + `commits/${f.source}`] = commit;
    const run = () => f.execute("", resolve("tools/security/monitor-maintenance.mts"));
    const healthy = run(); assert.equal(healthy.status, 0, healthy.stderr); assert.equal(healthy.writes, "");
    commit.commit.committer.date = new Date(Date.now() - 61 * 60 * 1000).toISOString();
    const stalled = run(); assert.notEqual(stalled.status, 0); assert(stalled.stderr.includes("has not published within one hour")); assert.equal(stalled.writes, "");
    commit.commit.committer.date = new Date(Date.now() + 120000).toISOString();
    assert.notEqual(run().status, 0);
    branch.object.sha = f.baseline;
    assert.equal(run().status, 0);
    f.routes[f.prefix + "git/matching-refs/heads/release/v0.1.6"] = [];
    assert.equal(run().status, 0);
  } finally { f.cleanup(); }
});

test("an ordinary main PR merge cannot create a release candidate", () => {
  const f = fixture();
  try {
    f.git("switch", "main"); f.run.head_branch = "main"; f.run.head_sha = f.main;
    f.routes[f.prefix + "git/ref/heads/main"] = { object: { sha: f.main } };
    f.routes[f.prefix + `commits/${f.main}/pulls`] = [{ ...f.pull, merge_commit_sha: f.main, base: { ref: "main" }, user: { login: "misaon", id: 15806356 } }];
    const result = f.execute(); assert.equal(result.status, 0, result.stderr); assert.equal(result.output, ""); assert.equal(result.writes, "");
  } finally { f.cleanup(); }
});

test("an older published scanner contract is deferred even when every dependency CI check passed", () => {
  const f = fixture();
  try {
    f.run.event = "pull_request"; f.pull.merged = false; f.pull.state = "open";
    f.routes[f.prefix + "git/ref/heads/release/v0.1.6"] = { object: { sha: f.baseline } };
    f.routes[f.prefix + `contents/tools/dependencies/assets.json?ref=${f.baseline}`].content = Buffer.from("{}").toString("base64");
    const result = f.execute("merge"); assert.equal(result.status, 0, result.stderr);
    assert.equal(result.writes, ""); assert.equal(result.output, ""); assert(result.stdout.includes("scanner bootstrap"));
    const tagSha = "d".repeat(40);
    f.routes[f.prefix + "git/ref/tags/v0.1.6"] = { object: { type: "tag", sha: tagSha } };
    f.routes[f.prefix + `git/tags/${tagSha}`] = { object: { type: "commit", sha: f.baseline } };
    const discovery = f.execute("", resolve("tools/dependencies/prepare.mts"));
    assert.equal(discovery.status, 0, discovery.stderr); assert.equal(discovery.output, "ready=false\n"); assert.equal(discovery.writes, "");
  } finally { f.cleanup(); }
});

test("artifact generation failures cannot merge even with green CI; another account cannot forge the warning", () => {
  const f = fixture();
  try {
    f.run.event = "pull_request"; f.pull.merged = false; f.pull.state = "open";
    f.routes[f.prefix + "git/ref/heads/release/v0.1.6"] = { object: { sha: f.baseline } };
    const warning = { user: f.pull.user, body: "### ⚠️ Artifact update problem\n\nFailed to regenerate a lockfile." };
    f.routes[f.prefix + "issues/7/comments?per_page=100"] = [warning];
    const blocked = f.execute("merge"); assert.equal(blocked.status, 0, blocked.stderr);
    assert(!blocked.writes.includes('"merge_method"'));
    assert(blocked.writes.includes("requires-attention"));
    const tagSha = "d".repeat(40);
    f.routes[f.prefix + "git/ref/tags/v0.1.6"] = { object: { type: "tag", sha: tagSha } };
    f.routes[f.prefix + `git/tags/${tagSha}`] = { object: { type: "commit", sha: f.baseline } };
    warning.user = { ...warning.user, id: 999 };
    const valid = f.execute("merge"); assert.equal(valid.status, 0, valid.stderr);
    assert(valid.writes.includes('"merge_method":"squash"'));
  } finally { f.cleanup(); }
});

test("failed dependency CI assigns maintainer attention, while stale heads are deferred", () => {
  const f = fixture();
  try {
    f.run.event = "pull_request"; f.run.conclusion = "failure";
    f.pull.merged = false; f.pull.state = "open";
    f.routes[f.prefix + "git/ref/heads/release/v0.1.6"] = { object: { sha: f.baseline } };
    const result = f.execute("merge"); assert.equal(result.status, 0, result.stderr);
    const writes = result.writes.trim().split("\n").map(line => JSON.parse(line));
    assert.deepEqual(writes.map(write => write.body), [{ labels: ["requires-attention", "automation:ci-failed"] }, { assignees: ["misaon"] }, { ref: "main", inputs: { mode: "security" } }]);
    assert(writes.every(write => !write.path.endsWith("/merge")));
    f.routes[f.prefix + "git/ref/heads/release/v0.1.6"] = { object: { sha: "f".repeat(40) } };
    const stale = f.execute("merge"); assert.equal(stale.status, 0, stale.stderr); assert.equal(stale.writes, ""); assert.equal(stale.output, "");
  } finally { f.cleanup(); }
});


test("only a CI-specific hold can clear after full verification; human holds and unsafe changes remain blocked", () => {
  const f = fixture();
  try {
    f.run.event = "pull_request"; f.pull.merged = false; f.pull.state = "open";
    f.routes[f.prefix + "git/ref/heads/release/v0.1.6"] = { object: { sha: f.baseline } };
    const tagSha = "d".repeat(40);
    f.routes[f.prefix + "git/ref/tags/v0.1.6"] = { object: { type: "tag", sha: tagSha } };
    f.routes[f.prefix + `git/tags/${tagSha}`] = { object: { type: "commit", sha: f.baseline } };
    f.pull.labels = [{ name: "requires-attention" }, { name: "automation:ci-failed" }];
    const recovered = f.execute("merge"); assert.equal(recovered.status, 0, recovered.stderr);
    const writes = recovered.writes.trim().split("\n").map(line => JSON.parse(line));
    assert.deepEqual(writes.map(write => write.method), ["DELETE", "DELETE", "PUT"]);
    assert.equal(writes[2].body.sha, f.source);
    f.pull.labels.push({ name: "manual-hold" });
    const manual = f.execute("merge"); assert.equal(manual.status, 0, manual.stderr); assert.equal(manual.writes, "");
    f.pull.labels = [{ name: "requires-attention" }];
    assert.equal(f.execute("merge").writes, "");
    f.pull.labels.push({ name: "automation:ci-failed" });
    f.routes[f.prefix + "actions/runs/123/jobs?per_page=100"].jobs[2].conclusion = "failure";
    assert.equal(f.execute("merge").writes, "");
    f.routes[f.prefix + "actions/runs/123/jobs?per_page=100"].jobs[2].conclusion = "success";
    f.routes[f.prefix + "pulls/7/files?per_page=100"].push({ filename: "internal/server/server.go", status: "modified" });
    f.pull.changed_files = 2;
    const unsafe = f.execute("merge"); assert.equal(unsafe.status, 0, unsafe.stderr);
    assert(unsafe.writes.includes("manual-hold")); assert(!unsafe.writes.includes('"DELETE"')); assert(!unsafe.writes.includes('"PUT"'));
  } finally { f.cleanup(); }
});

test("a maintenance merge cancels only unchanged signed App PR CI from the superseded base", () => {
  const f = fixture();
  try {
    f.run.event = "pull_request"; f.pull.merged = false; f.pull.state = "open";
    f.routes[f.prefix + "git/ref/heads/release/v0.1.6"] = { object: { sha: f.baseline } };
    const tagSha = "d".repeat(40);
    f.routes[f.prefix + "git/ref/tags/v0.1.6"] = { object: { type: "tag", sha: tagSha } };
    f.routes[f.prefix + `git/tags/${tagSha}`] = { object: { type: "commit", sha: f.baseline } };
    const sha = "e".repeat(40), humanSha = "f".repeat(40);
    const candidate = { ...f.pull, number: 8, head: { ...f.pull.head, sha, ref: "renovate/stale" } };
    const human = { ...candidate, number: 9, head: { ...candidate.head, sha: humanSha } };
    f.routes[f.prefix + "pulls?state=open&per_page=100"] = [candidate, human, { ...candidate, base: { ref: "main" } }, { ...candidate, draft: true }, { ...candidate, labels: [{ name: "manual-hold" }] }];
    f.routes[f.prefix + `commits/${sha}`] = { author: f.pull.user, commit: { verification: { verified: true } }, parents: [{ sha: f.baseline }] };
    f.routes[f.prefix + `commits/${humanSha}`] = { author: { login: "misaon", id: 999 }, commit: { verification: { verified: true } }, parents: [{ sha: f.baseline }] };
    const stale = { id: 456, workflow_id: f.run.workflow_id, event: "pull_request", head_repository: f.pull.head.repo, head_sha: sha, head_branch: candidate.head.ref, status: "in_progress" };
    f.routes[f.prefix + `actions/workflows/ci.yml/runs?head_sha=${sha}&event=pull_request&per_page=100`] = { workflow_runs: [
      stale, { ...stale, id: 457, event: "push" }, { ...stale, id: 458, workflow_id: 999 },
      { ...stale, id: 459, head_repository: { full_name: "other/fork" } }, { ...stale, id: 460, status: "completed" },
    ] };
    const result = f.execute("merge"); assert.equal(result.status, 0, result.stderr);
    const writes = result.writes.trim().split("\n").map(line => JSON.parse(line));
    assert.deepEqual(writes.map(write => write.path), [f.prefix + "pulls/7/merge", f.prefix + "actions/runs/456/cancel", f.prefix + "pulls/8"]);
    assert.equal(writes[0].dispatchToken, false); assert.equal(writes[1].dispatchToken, true);
    assert.deepEqual(writes[2].body, { state: "closed" });
  } finally { f.cleanup(); }
});

test("discovery prunes superseded signed proposals before budgeting and preserves human migration work", () => {
  const f = fixture();
  try {
    const tagSha = "d".repeat(40);
    f.routes[f.prefix + "git/ref/heads/main"] = { object: { sha: f.main } };
    f.routes[f.prefix + "git/ref/tags/v0.1.6"] = { object: { type: "tag", sha: tagSha } };
    f.routes[f.prefix + `git/tags/${tagSha}`] = { object: { type: "commit", sha: f.baseline } };
    f.routes[f.prefix + "git/matching-refs/heads/release/v0.1.6"] = [{ ref: "refs/heads/release/v0.1.6", object: { sha: f.baseline } }];
    const old = { ...f.pull, number: 8, state: "open", labels: [{ name: "requires-attention" }], base: { ref: "release/v0.1.5" } };
    const human = { ...old, number: 9 };
    const main = { ...old, number: 10, base: { ref: "main" } };
    f.routes[f.prefix + "pulls?state=open&per_page=100"] = [old, human, main];
    const signed = { sha: f.pull.head.sha, author: f.pull.user, commit: { verification: { verified: true } }, parents: [{ sha: f.baseline }] };
    f.routes[f.prefix + "pulls/8/commits?per_page=100"] = [signed];
    f.routes[f.prefix + "pulls/9/commits?per_page=100"] = [signed, { ...signed, author: { login: "misaon", id: 999 } }];
    const result = f.execute("", resolve("tools/dependencies/prepare.mts"));
    assert.equal(result.status, 0, result.stderr);
    const writes = result.writes.trim().split("\n").map(line => JSON.parse(line));
    assert.deepEqual(writes.map(write => ({ path: write.path, method: write.method, body: write.body })), [{ path: f.prefix + "pulls/8", method: "PATCH", body: { state: "closed" } }]);
    assert(result.output.includes("blocked=2\n"), "Only retained human work and the main hold need budget compensation");
  } finally { f.cleanup(); }
});

test("discovery retires only stale verified evidence and rechecks human edits immediately before closing", () => {
  const f = fixture();
  try {
    const tagSha = "d".repeat(40), passedHead = "e".repeat(40), failedHead = "f".repeat(40);
    f.routes[f.prefix + "git/ref/heads/main"] = { object: { sha: f.main } };
    f.routes[f.prefix + "git/ref/tags/v0.1.6"] = { object: { type: "tag", sha: tagSha } };
    f.routes[f.prefix + `git/tags/${tagSha}`] = { object: { type: "commit", sha: f.baseline } };
    f.routes[f.prefix + "git/matching-refs/heads/release/v0.1.6"] = [{ ref: "refs/heads/release/v0.1.6", object: { sha: f.baseline } }];
    const candidates = ["first", "second"].map(name => ({ name, manager: "npm", datasource: "npm", value: "1.2.4", version: "1.2.4" }));
    const batch = { base: "main", mode: "weekly", phase: "batch", root: 0, head: "", candidates };
    const root = { ...f.pull, number: 10, state: "open", base: { ...f.pull.base, ref: "main" }, head: { ...f.pull.head, ref: "renovate/main-weekly-dependency-batch" }, labels: [{ name: "automation:batch-failed" }, { name: "automation:ci-failed" }, { name: "requires-attention" }] };
    const passed = { ...root, number: 11, head: { ...root.head, sha: passedHead, ref: `renovate/diagnose-10-${batches.unitId("first")}` }, labels: [{ name: "automation:diagnostic-passed" }] };
    const failed = { ...root, number: 12, head: { ...root.head, sha: failedHead, ref: `renovate/diagnose-10-${batches.unitId("second")}` }, labels: [{ name: "automation:diagnostic-failed" }, { name: "automation:ci-failed" }, { name: "requires-attention" }] };
    f.routes[f.prefix + "pulls?state=open&per_page=100"] = [root, passed, failed];
    for (const [pull, metadata, id, conclusion] of [
      [root, batch, 200, "failure"],
      [passed, { ...batch, phase: "diagnostic", root: 10, head: root.head.sha, candidates: [candidates[0]] }, 201, "success"],
      [failed, { ...batch, phase: "diagnostic", root: 10, head: root.head.sha, candidates: [candidates[1]] }, 202, "failure"],
    ] as const) {
      f.routes[f.prefix + `pulls/${pull.number}`] = pull;
      f.routes[f.prefix + `pulls/${pull.number}/commits?per_page=100`] = [{ sha: pull.head.sha, author: pull.user, parents: [{ sha: f.baseline }], commit: { verification: { verified: true }, message: `fix(deps): test\n\nNylzo-Batch-V1: ${JSON.stringify(metadata)}` } }];
      f.routes[f.prefix + `issues/${pull.number}/comments?per_page=100`] = [];
      f.routes[f.prefix + `actions/workflows/ci.yml/runs?head_sha=${pull.head.sha}&event=pull_request&per_page=100`] = { workflow_runs: [{ id, event: "pull_request", status: "completed", conclusion, head_sha: pull.head.sha, head_repository: pull.head.repo }] };
      f.routes[f.prefix + `actions/runs/${id}/jobs?per_page=100`] = f.routes[f.prefix + "actions/runs/123/jobs?per_page=100"];
    }
    let result = f.execute("", resolve("tools/dependencies/prepare.mts"));
    assert.equal(result.status, 0, result.stderr);
    const closed = () => result.writes.trim().split("\n").filter(Boolean).map(line => JSON.parse(line).path);
    assert.deepEqual(closed(), [f.prefix + "pulls/10", f.prefix + "pulls/11"]);
    assert(result.output.includes("blocked=1\n"), "The individual failure remains open and quarantined");
    f.routes[f.prefix + "pulls/11"] = { ...passed, labels: [{ name: "manual-hold" }] };
    result = f.execute("", resolve("tools/dependencies/prepare.mts"));
    assert.equal(result.status, 0, result.stderr); assert.deepEqual(closed(), [f.prefix + "pulls/10"]);
    f.routes[f.prefix + "pulls/11"] = { ...passed, head: { ...passed.head, sha: "1".repeat(40) } };
    result = f.execute("", resolve("tools/dependencies/prepare.mts"));
    assert.equal(result.status, 0, result.stderr); assert.deepEqual(closed(), [f.prefix + "pulls/10"]);
  } finally { f.cleanup(); }
});

test("a verified main dependency merge refills discovery without authorizing a release", () => {
  const f = fixture();
  try {
    f.run.event = "pull_request"; f.pull.merged = false; f.pull.state = "open";
    f.pull.base.ref = "main"; f.pull.labels = [{ name: "automation:weekly" }];
    f.routes[f.prefix + "git/ref/heads/main"] = { object: { sha: f.baseline } };
    const result = f.execute("merge"); assert.equal(result.status, 0, result.stderr);
    const writes = result.writes.trim().split("\n").map(line => JSON.parse(line));
    assert.deepEqual(writes.map(write => write.path), [f.prefix + "pulls/7/merge", f.prefix + "actions/workflows/dependencies.yml/dispatches"]);
    assert.deepEqual(writes[1].body, { ref: "main", inputs: { mode: "weekly" } });
    assert.equal(writes[1].dispatchToken, true); assert.equal(result.output, "");
  } finally { f.cleanup(); }
});

test("a planned feature release numbers above isolated patches and requires exact release metadata", () => {
  const f = fixture();
  try {
    f.git("tag", "v0.1.7");
    f.routes[f.prefix + "releases/latest"].tag_name = "v0.1.7";
    f.routes[f.prefix + "releases?per_page=100"] = ["v0.1.7", "v0.1.6"].map(tag_name => ({ tag_name, immutable: true, draft: false, prerelease: false }));
    f.git("switch", "main");
    const planned = f.execute("", resolve("tools/release/plan.mts"));
    assert.equal(planned.status, 0, planned.stderr);
    assert(planned.output.includes("version=0.2.0\n"));
    assert(planned.output.includes(`baseline=${f.baseline}\n`));
    assert(!planned.output.includes(f.source), "An isolated patch is not the main changelog baseline");
    writeFileSync(join(f.root, ".release-please-manifest.json"), '{".":"0.2.0"}');
    f.git("add", ".release-please-manifest.json"); f.git("commit", "-m", "chore(main): release 0.2.0");
    const merged = f.git("rev-parse", "HEAD");
    f.run.head_branch = "main"; f.run.head_sha = merged;
    f.routes[f.prefix + "git/ref/heads/main"] = { object: { sha: merged } };
    const pull = { ...f.pull, title: "chore(main): release 0.2.0", merge_commit_sha: merged, base: { ...f.pull.base, ref: "main" }, head: { ...f.pull.head, ref: "release-please--branches--main" } };
    f.routes[f.prefix + `commits/${merged}/pulls`] = [pull];
    f.routes[f.prefix + "pulls/7"] = pull;
    f.routes[f.prefix + "pulls/7/files?per_page=100"] = [{ filename: ".release-please-manifest.json", status: "modified" }];
    f.routes[f.prefix + `contents/.release-please-manifest.json?ref=${merged}`] = { type: "file", encoding: "base64", content: Buffer.from('{".":"0.2.0"}').toString("base64") };
    const candidate = f.execute(); assert.equal(candidate.status, 0, candidate.stderr);
    assert(candidate.output.includes("tag=v0.2.0\n")); assert(candidate.output.includes("kind=planned\n"));
    f.routes[f.prefix + "git/ref/heads/main"] = { object: { sha: "e".repeat(40) } };
    f.routes[f.prefix + `compare/${merged}...${"e".repeat(40)}`] = { status: "ahead" };
    const laterMain = f.execute(); assert.equal(laterMain.status, 0, laterMain.stderr);
    assert(laterMain.output.includes(`commit=${merged}\n`), "Changes merged during release CI must stay outside this release");
    f.routes[f.prefix + `compare/${merged}...${"e".repeat(40)}`].status = "diverged";
    assert.notEqual(f.execute().status, 0, "A rewritten main ancestry cannot authorize the old release");
    f.routes[f.prefix + "git/ref/heads/main"] = { object: { sha: merged } };
    pull.title = "chore(main): release 0.1.8";
    assert.notEqual(f.execute().status, 0, "Stale release numbering cannot authorize publication");
  } finally { f.cleanup(); }
});


test("publication drains the remaining weekly updates and failed publication assigns attention", () => {
  const f = fixture();
  try {
    f.run.workflow_id = 99; f.run.head_branch = "v0.1.7";
    f.routes[f.prefix + "actions/workflows/release.yml"] = { id: 99 };
    f.routes[f.prefix + "releases/tags/v0.1.7"] = { immutable: true, draft: false, prerelease: false };
    f.routes[f.prefix + "git/ref/tags/v0.1.7"] = { object: { type: "tag", sha: "d".repeat(40) } };
    f.routes[f.prefix + `git/tags/${"d".repeat(40)}`] = { object: { type: "commit", sha: f.source }, message: "Nylzo 0.1.7\n\nRelease-Kind: weekly\n" };
    const result = f.execute("", resolve("tools/release/published.mts"));
    assert.equal(result.status, 0, result.stderr);
    const writes = result.writes.trim().split("\n").map(line => JSON.parse(line));
    assert.deepEqual(writes.map(write => write.body), [{ ref: "main" }, { ref: "main", inputs: { mode: "weekly" } }]);
    assert(writes.every(write => write.path.endsWith("/dispatches")));
    f.run.conclusion = "failure";
    const failed = f.execute("", resolve("tools/release/published.mts"));
    assert.equal(failed.status, 0, failed.stderr);
    assert.deepEqual(failed.writes.trim().split("\n").map(line => JSON.parse(line).body), [{ labels: ["requires-attention"] }, { assignees: ["misaon"] }]);
  } finally { f.cleanup(); }
});


test("changing an Action version comment cannot disguise an older pinned commit", () => {
  const f = fixture();
  try {
    const path = ".github/workflows/ci.yml", digest = "a".repeat(40);
    const before = `uses: actions/checkout@${digest} # v7.0.1\n`;
    const after = before.replace("v7.0.1", "v7.0.2");
    f.routes[f.prefix + "pulls/7/files?per_page=100"] = [{ filename: path, status: "modified" }];
    for (const [sha, text] of [[f.baseline, before], [f.source, after]]) f.routes[f.prefix + `contents/${path}?ref=${sha}`] = { type: "file", encoding: "base64", content: Buffer.from(text!).toString("base64") };
    f.routes["repos/actions/checkout/git/ref/tags/v7.0.2"] = { object: { type: "commit", sha: "b".repeat(40) } };
    const forged = f.execute(); assert.notEqual(forged.status, 0); assert.equal(forged.output, "");
    f.routes["repos/actions/checkout/git/ref/tags/v7.0.2"].object.sha = digest;
    const genuineAlias = f.execute(); assert.equal(genuineAlias.status, 0, genuineAlias.stderr);
    assert(genuineAlias.output.includes("tag=v0.1.7\n"));
  } finally { f.cleanup(); }
});


test("an intervening patch regenerates stale planned metadata without publishing or bypassing approval", () => {
  const f = fixture();
  try {
    f.git("tag", "v0.1.7");
    f.routes[f.prefix + "releases/latest"].tag_name = "v0.1.7";
    f.routes[f.prefix + "releases?per_page=100"] = ["v0.1.7", "v0.1.6"].map(tag_name => ({ tag_name, immutable: true, draft: false, prerelease: false }));
    f.git("switch", "main"); f.git("commit", "--amend", "-m", "fix: pending repair");
    writeFileSync(join(f.root, ".release-please-manifest.json"), '{".":"0.1.7"}');
    f.git("add", ".release-please-manifest.json"); f.git("commit", "-m", "chore(main): release 0.1.7");
    const merged = f.git("rev-parse", "HEAD");
    f.run.head_branch = "main"; f.run.head_sha = merged;
    f.routes[f.prefix + "git/ref/heads/main"] = { object: { sha: merged } };
    const pull = { ...f.pull, title: "chore(main): release 0.1.7", labels: [{ name: "autorelease: pending" }], merge_commit_sha: merged, base: { ...f.pull.base, ref: "main" }, head: { ...f.pull.head, ref: "release-please--branches--main" } };
    f.routes[f.prefix + `commits/${merged}/pulls`] = [pull]; f.routes[f.prefix + "pulls/7"] = pull;
    f.routes[f.prefix + "pulls/7/files?per_page=100"] = [{ filename: ".release-please-manifest.json", status: "modified" }];
    f.routes[f.prefix + `contents/.release-please-manifest.json?ref=${merged}`] = { type: "file", encoding: "base64", content: Buffer.from('{".":"0.1.7"}').toString("base64") };
    const result = f.execute(); assert.equal(result.status, 0, result.stderr); assert.equal(result.output, "");
    const writes = result.writes.trim().split("\n").map(line => JSON.parse(line));
    assert.deepEqual(writes.map(write => write.method), ["POST", "POST", "DELETE", "POST"]);
    assert(writes.at(-1).path.endsWith("/release-please.yml/dispatches"));
    assert(writes.every(write => !write.path.endsWith("/merge") && !write.path.endsWith("/git/refs")));
  } finally { f.cleanup(); }
});
