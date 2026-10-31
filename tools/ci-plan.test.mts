import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { changedFiles, checksPassed, planChecks } from "./ci-plan.mts";

test("documentation skips application tests and builds while consumed protocol and unknown files require full coverage", () => {
  const documentation = planChecks(
    ["README.md", "docs/wiki/guides/CLI.md", "assets/readme/cover-dark.svg"],
    "pull_request",
  );
  assert.ok(Object.entries(documentation.jobs).every(([job, required]) => required === job.startsWith("codeql-")));
  assert.ok(Object.values(planChecks(["README.md"], "push").jobs).every((required) => !required));
  for (const file of [
    "docs/wiki/reference/Protocol.md",
    "spec/vectors.json",
    "internal/core/open.go",
    "web/core/src/open.ts",
    ".github/workflows/ci.yml",
    "new-runtime/config.json",
    "README.md\ninternal/core/open.go",
  ]) {
    assert.equal(planChecks([file], "pull_request").full, true, file);
  }
});

test("Go and web changes retain integration coverage and their language security analysis", () => {
  const go = planChecks(["internal/server/router.go"], "pull_request");
  assert.equal(go.jobs.go, true);
  assert.equal(go.jobs["codeql-go"], true);
  assert.equal(go.jobs.e2e, true);
  assert.equal(go.jobs.web, false);
  const web = planChecks(["web/app/src/App.svelte"], "push");
  assert.equal(web.jobs.web, true);
  assert.equal(web.jobs["codeql-web"], true);
  assert.equal(web.jobs.build, true);
  assert.equal(web.jobs.go, false);
  assert.equal(planChecks(["web/package.json"], "pull_request").dependencies, true);
});

test("pushes, mixed changes, renames, and full scans cannot hide executable changes behind Markdown", () => {
  assert.equal(planChecks(["README.md"], "push").full, false);
  for (const files of [
    ["README.md", "internal/core/open.go"],
    ["internal/core/deleted.go", "docs/wiki/guides/renamed.md"],
    Array.from({ length: 400 }, (_, index) => `docs/wiki/${index}.md`).concat("go.mod"),
  ]) {
    assert.equal(planChecks(files, "pull_request").full, true);
  }
  for (const event of ["schedule", "workflow_dispatch"]) {
    const plan = planChecks([], event);
    assert.equal(plan.full, true);
    assert.ok(Object.values(plan.jobs).every(Boolean));
  }
  assert.throws(() => planChecks([null] as unknown as string[], "pull_request"));
  assert.throws(() => planChecks([], "unexpected"));
});

test("the required gate rejects failed classification, missing outputs, and every unexpected job result", () => {
  for (const event of ["pull_request", "push", "schedule"]) {
    const plan = planChecks(["README.md"], event);
    const results: Record<string, { result: string; outputs?: Record<string, string> }> = Object.fromEntries(
      Object.entries(plan.jobs).map(([job, required]) => [job, { result: required ? "success" : "skipped" }]),
    );
    results.changes = { result: "success", outputs: { jobs: JSON.stringify(plan.jobs) } };
    assert.equal(checksPassed(results), true);
    for (const job of Object.keys(results)) {
      for (const result of ["failure", "cancelled", "", results[job].result === "success" ? "skipped" : "success"]) {
        assert.equal(
          checksPassed({ ...results, [job]: { ...results[job], result } }),
          false,
          `${event}: ${job} ${result}`,
        );
      }
      const missing = { ...results };
      delete missing[job];
      assert.equal(checksPassed(missing), false);
    }
    for (const jobs of ["", "null", "{}", "[]", '{"web":"false"}']) {
      assert.equal(checksPassed({ ...results, changes: { result: "success", outputs: { jobs } } }), false);
    }
  }
});

test("the isolated Flatcar profile runs provisioning checks without unrelated application suites", () => {
  for (const file of ["deploy/hetzner-flatcar/runtime/update", "tools/deploy/render.mts", "mise-tasks/deploy"]) {
    const plan = planChecks([file], "pull_request");
    assert.equal(plan.jobs.deployment, true);
    assert.equal(plan.jobs.repository, true);
    assert.equal(plan.jobs.go, false);
    assert.equal(plan.jobs.e2e, false);
  }
});

test("fork PRs keep tests and reports while the OIDC publishing job stays skipped", () => {
  const plan = planChecks(["web/app/src/App.svelte", "internal/server/router.go"], "pull_request", false);
  assert.equal(plan.jobs.web, true);
  assert.equal(plan.jobs.go, true);
  assert.equal(plan.jobs.coverage, false);
  assert.equal(plan.coverage, false);
  assert.equal(planChecks(["web/app/src/App.svelte"], "pull_request", true).jobs.coverage, true);
});

test("the release PR runs only the documentation checks, while its merge to main runs full verification", () => {
  const release = [".github/CHANGELOG.md", ".release-please-manifest.json"];
  const pullRequest = planChecks(release, "pull_request");
  assert.ok(Object.entries(pullRequest.jobs).every(([job, required]) => required === job.startsWith("codeql-")));
  assert.equal(planChecks([".release-please-manifest.json"], "pull_request").full, false);
  assert.equal(planChecks(release, "push").full, true);
  assert.equal(planChecks([...release, "internal/server/router.go"], "pull_request").full, true);
});

test("changed files include deleted paths and both sides of a rename; an unknown base selects everything", () => {
  const repository = mkdtempSync(join(tmpdir(), "nylzo-ci-plan-"));
  const git = (...args: string[]) =>
    execFileSync("git", ["-c", "user.name=CI", "-c", "user.email=ci@example.org", "-c", "commit.gpgsign=false", ...args], {
      cwd: repository,
      encoding: "utf8",
    }).trim();
  try {
    git("init", "--quiet");
    mkdirSync(join(repository, "docs"));
    writeFileSync(join(repository, "router.go"), "package server\n");
    writeFileSync(join(repository, "docs/old name.md"), "# Old\n");
    git("add", ".");
    git("commit", "--quiet", "-m", "base");
    const base = git("rev-parse", "HEAD");
    git("mv", "router.go", "moved.go");
    git("rm", "--quiet", "docs/old name.md");
    git("commit", "--quiet", "-m", "head");
    const head = git("rev-parse", "HEAD");
    assert.deepEqual(changedFiles(base, head, repository).toSorted(), ["docs/old name.md", "moved.go", "router.go"]);
    assert.deepEqual(changedFiles("0".repeat(40), head, repository), []);
    assert.deepEqual(changedFiles("f".repeat(40), head, repository), []);
    assert.throws(() => changedFiles("main", head, repository));
  } finally {
    rmSync(repository, { recursive: true, force: true });
  }
});
