import assert from "node:assert/strict";
import { appendFileSync, chmodSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { externalArtifacts, prepareExternal } from "./external.mts";

// Use protected-main pins, not a candidate branch or an unpinned runtime install.
assert.equal(process.env.GITHUB_REF, "refs/heads/main");
assert.equal(process.env.GITHUB_REPOSITORY, "misaon/nylzo");
const artifact = externalArtifacts(path => readFileSync(path, "utf8")).find(item => item.name === "external-mise-amd64");
assert(artifact);
const directory = mkdtempSync(join(tmpdir(), "nylzo-renovate-tools-"));
const binary = await prepareExternal(artifact, directory);
// Docker mounts this verified file read-only for its unprivileged UID.
chmodSync(directory, 0o755);
chmodSync(binary, 0o555);
appendFileSync(process.env.GITHUB_OUTPUT!, `mise=${binary}\n`);
