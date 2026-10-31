import assert from "node:assert/strict";
import test from "node:test";
import { checksPassed, planChecks } from "./ci-plan.mts";

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
  assert.equal(planChecks(["web/package.json"], "pull_request").jobs["dependency-review"], true);
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
    assert.ok(
      Object.entries(plan.jobs).every(([job, required]) => (job === "dependency-review" ? !required : required)),
    );
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
