import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { appendFileSync } from "node:fs";
import { nextVersion, versionParts } from "./policy.mts";
import { changesAfter, lastMainRelease } from "./baseline.mts";

const latest = JSON.parse(execFileSync("gh", ["api", "repos/misaon/nylzo/releases/latest"], { encoding: "utf8" }));
assert.equal(latest.immutable, true);
assert.equal(latest.draft, false);
assert.equal(latest.prerelease, false);
versionParts(latest.tag_name);
const baseline = lastMainRelease();
const commits = changesAfter(baseline.sha);
const version = nextVersion(latest.tag_name, commits);
if (version) {
  // The manifest identifies the last planned main release. Isolated production
  // patch tags do not advance it. Use the latest published version for numbering.
  appendFileSync(process.env.GITHUB_OUTPUT!, `version=${version}\nbaseline=${baseline.sha}\nbaseline_version=${baseline.tag.slice(1)}\n`);
  console.log(`Prepared release PR version ${version}. No tag or release created.`);
}
