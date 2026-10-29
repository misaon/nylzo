import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { GitHub, Manifest } from "release-please";
import { Version } from "release-please/build/src/version.js";

assert.equal(process.env.GITHUB_REPOSITORY, "misaon/nylzo");
assert.equal(process.env.GITHUB_REF, "refs/heads/main");
assert.match(process.env.RELEASE_VERSION!, /^\d+\.\d+\.\d+$/u);
assert.match(process.env.RELEASE_BASELINE!, /^[a-f0-9]{40}$/u);
assert.match(process.env.RELEASE_BASELINE_VERSION!, /^\d+\.\d+\.\d+$/u);
const settings = JSON.parse(readFileSync("release-please-config.json", "utf8"));
const component = settings.packages["."];
const github = await GitHub.create({ owner: "misaon", repo: "nylzo", token: process.env.GH_TOKEN! });
const configuration = await Manifest.fromConfig(github, "main", {
  releaseType: "go", releaseAs: process.env.RELEASE_VERSION,
  includeComponentInTag: false, includeVInTag: true,
  bumpMinorPreMajor: false, bumpPatchForMinorPreMajor: false,
  changelogPath: component["changelog-path"], changelogSections: component["changelog-sections"],
  pullRequestHeader: settings["pull-request-header"], pullRequestFooter: settings["pull-request-footer"],
}, { lastReleaseSha: process.env.RELEASE_BASELINE });
const manifest = new Manifest(github, "main", configuration.repositoryConfig,
  { ".": Version.parse(process.env.RELEASE_BASELINE_VERSION!) },
  { lastReleaseSha: process.env.RELEASE_BASELINE });
// Creation/update of the aggregate PR is the only authorized write operation.
// Tagging, publication and merging are handled by separate verified workflows.
if (process.argv.includes("--dry-run")) {
  const candidates = await manifest.buildPullRequests();
  console.log(JSON.stringify(candidates.map(candidate => ({ title: candidate.title.toString(), branch: candidate.headRefName, files: candidate.updates.map(update => update.path) }))));
} else await manifest.createPullRequests();
