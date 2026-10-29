import assert from "node:assert/strict";
import test from "node:test";
import { nextVersion, releaseBranch, releaseMetadata } from "./policy.mts";

test("Conventional Commits use patch, minor and major even before 1.0", () => {
  assert.equal(nextVersion("v0.1.6", ["fix(cli): correct output", "docs: add example"]), "0.1.7");
  assert.equal(nextVersion("v0.1.9", ["feat(web): add option", "fix: correct output"]), "0.2.0");
  assert.equal(nextVersion("v0.1.9", ["refactor!: change protocol"]), "1.0.0");
  assert.equal(nextVersion("v2.3.4", ["fix: change output\n\nBREAKING CHANGE: remove a field"]), "3.0.0");
  assert.equal(nextVersion("v2.3.4", ["chore(main): release 2.3.4"]), undefined);
  assert.equal(releaseBranch("v0.1.6"), "release/v0.1.6");
  assert.throws(() => releaseBranch("v0.01.6"));
});

test("a release label alone cannot authorize publishing arbitrary application changes", () => {
  process.env.DEPENDENCY_APP_SLUG = "nylzo-dependency-updates";
  process.env.DEPENDENCY_BOT_ID = "123456";
  const pull = { title: "chore(main): release 0.2.0", merged: true,
    user: { login: "nylzo-dependency-updates[bot]", id: 123456 },
    head: { repo: { full_name: "misaon/nylzo" }, ref: "release-please--branches--main" },
    base: { repo: { full_name: "misaon/nylzo" }, ref: "main" } };
  const files = [{ filename: ".release-please-manifest.json", status: "modified" }];
  assert.equal(releaseMetadata(pull, files, '{".":"0.2.0"}'), "v0.2.0");
  for (const mutate of [
    (x: any) => x.user.id = 1,
    (x: any) => x.head.repo.full_name = "attacker/nylzo",
    (x: any) => x.head.ref = "renovate/example",
    (x: any) => x.base.ref = "release/v0.1.6",
    (x: any) => x.title = "chore(main): release 0.1.7",
  ]) { const changed = structuredClone(pull); mutate(changed); assert.throws(() => releaseMetadata(changed, files, '{".":"0.2.0"}')); }
  assert.throws(() => releaseMetadata(pull, [...files, { filename: "internal/server/server.go", status: "modified" }], '{".":"0.2.0"}'));
});
