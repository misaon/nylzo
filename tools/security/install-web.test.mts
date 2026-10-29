import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

for (const updater of ["dependabot", "renovate"]) test(`${updater}: fresh security fixes get only an exact version exception, restored even when installation fails`, () => {
  const root = mkdtempSync(join(tmpdir(), "nylzo-age-policy-"));
  try {
    for (const directory of ["web", "tools/security", "tools/sca", "tools/release", "tools/dependencies", "bin"]) mkdirSync(join(root, directory), { recursive: true });
    // The installer is mocked in this age-policy test; patch integrity has its own checks.
    writeFileSync(join(root, "tools/sca/deepmerge.mts"), 'export function verifyDeepmergePatch() { if (process.env.PATCH_STATUS === "fail") throw new Error("Invalid patch"); }\n');
    for (const name of ["install-web.mts", "dependency-api.mts", "dependency-policy.mts"]) cpSync(`tools/security/${name}`, join(root, "tools/security", name));
    for (const name of ["policy.mts", "github.mts"]) cpSync(`tools/release/${name}`, join(root, "tools/release", name));
    for (const name of ["age.mts", "inspect.mts", "policy.mts"]) cpSync(`tools/dependencies/${name}`, join(root, "tools/dependencies", name));
    const original = "packages: [app]\nminimumReleaseAge: 1440\nstrictDepBuilds: true\nallowBuilds: {}\n";
    writeFileSync(join(root, "web/pnpm-workspace.yaml"), original);
    const bot = updater === "dependabot" ? { login: "dependabot[bot]", id: 49699333 } : { login: "nylzo-dependency-updates[bot]", id: 349732443 };
    const source = { full_name: "misaon/nylzo" };
    const base = "b".repeat(40), head = "a".repeat(40);
    const pull = { number: 1, draft: false, user: bot, head: { sha: head, ref: updater === "dependabot" ? "dependabot/npm_and_yarn/web/example-1.2.4" : "renovate/example-1.x", repo: source }, base: { ref: "main", sha: base, repo: source }, changed_files: 1, merged: false, state: "open" };
    const commit = { sha: head, author: bot, parents: [{ sha: base }], commit: { verification: { verified: true }, message: "Bumps example from 1.2.3 to 1.2.4.\n---\nupdated-dependencies:\n- dependency-name: example\n...\n" } };
    const routes = {
      "repos/misaon/nylzo/pulls/1": pull,
      "repos/misaon/nylzo/pulls/1/commits?per_page=100": [[commit]],
      "repos/misaon/nylzo/pulls/1/files?per_page=100": [[{ filename: "web/app/package.json", status: "modified" }]],
      "repos/misaon/nylzo/git/ref/heads/main": { object: { sha: base } },
      [`repos/misaon/nylzo/contents/web/app/package.json?ref=${base}`]: { type: "file", encoding: "base64", content: Buffer.from('{"dependencies":{"example":"1.2.3"}}').toString("base64") },
      [`repos/misaon/nylzo/contents/web/app/package.json?ref=${head}`]: { type: "file", encoding: "base64", content: Buffer.from('{"dependencies":{"example":"1.2.4"}}').toString("base64") },
      "advisories?type=reviewed&ecosystem=npm&affects=example%401.2.3&per_page=100": [[{ ghsa_id: "GHSA-test-test-test", vulnerabilities: [{ package: { name: "example", ecosystem: "npm" }, first_patched_version: "1.2.4" }] }]],
      "advisories?type=reviewed&ecosystem=npm&affects=example%401.2.4&per_page=100": [[]],
    };
    writeFileSync(join(root, "event.json"), JSON.stringify({ pull_request: pull }));
    writeFileSync(join(root, "bin/gh"), '#!/usr/bin/env node\nconst routes = JSON.parse(process.env.API_ROUTES); const path = process.argv.find(value => value.startsWith("repos/") || value.startsWith("advisories?")); if (!(path in routes)) process.exit(1); console.log(JSON.stringify(routes[path]));\n');
    writeFileSync(join(root, "bin/pnpm"), '#!/usr/bin/env node\nconst fs = require("node:fs"); fs.copyFileSync(process.env.WORKSPACE,process.env.CAPTURE); process.exit(Number(process.env.INSTALL_STATUS || 0));\n');
    for (const binary of ["gh", "pnpm"]) chmodSync(join(root, "bin", binary), 0o700);
    const run = (status: string, values = routes, patchStatus = "pass") => spawnSync(process.execPath, [join(root, "tools/security/install-web.mts")], { encoding: "utf8", env: {
      ...process.env, DEPENDENCY_APP_SLUG: "nylzo-dependency-updates", DEPENDENCY_BOT_ID: "349732443", PATH: `${root}/bin:${process.env.PATH}`, GITHUB_ACTIONS: "true", GITHUB_REPOSITORY: "misaon/nylzo", GH_TOKEN: "test-only", GITHUB_EVENT_PATH: join(root, "event.json"), API_ROUTES: JSON.stringify(values), WORKSPACE: join(root, "web/pnpm-workspace.yaml"), CAPTURE: join(root, "observed.yaml"), INSTALL_STATUS: status, PATCH_STATUS: patchStatus,
    }});
    assert.equal(run("13").status, 13);
    assert.equal(readFileSync(join(root, "web/pnpm-workspace.yaml"), "utf8"), original);
    const observed = readFileSync(join(root, "observed.yaml"), "utf8");
    assert(observed.includes('minimumReleaseAgeExclude:\n  - "example@1.2.4"'));
    assert(observed.includes("minimumReleaseAge: 1440"));
    const forged = structuredClone(routes); forged["repos/misaon/nylzo/pulls/1"].user.id = 42;
    assert.equal(run("0", forged).status, 0);
    assert.equal(readFileSync(join(root, "observed.yaml"), "utf8"), original);
    assert.notEqual(run("0", routes, "fail").status, 0);
    assert.equal(readFileSync(join(root, "web/pnpm-workspace.yaml"), "utf8"), original);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
