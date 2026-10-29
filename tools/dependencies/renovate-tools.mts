import assert from "node:assert/strict";
import { appendFileSync, chmodSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { externalArtifacts, prepareExternal } from "./external.mts";
import type { ExternalArtifact } from "./external.mts";
import { contents } from "../release/github.mts";
import { versionParts } from "../release/policy.mts";

export function selectMise(candidates: ExternalArtifact[]): ExternalArtifact {
  assert(candidates.length > 0);
  const compare = (left: number[], right: number[]) => {
    for (let index = 0; index < 3; index++) if (left[index] !== right[index]) return left[index]! - right[index]!;
    return 0;
  };
  for (const candidate of candidates) {
    assert.equal(candidate.name, "external-mise-amd64");
    assert(compare(versionParts(candidate.version), [2026, 7, 12]) >= 0, "Mise must support safe lock generation");
  }
  return [...candidates].sort((left, right) => compare(versionParts(right.version), versionParts(left.version)))[0]!;
}

if (import.meta.main) {
  // Neither a candidate branch nor the latest upstream tag chooses this tool.
  // Published maintenance fixes may already be ahead of protected main.
  assert.equal(process.env.GITHUB_REF, "refs/heads/main");
  assert.equal(process.env.GITHUB_REPOSITORY, "misaon/nylzo");
  const publishedAssets = contents("tools/dependencies/assets.json", process.env.NYLZO_BASE_SOURCE!);
  const read = (path: string) => readFileSync(path, "utf8");
  const candidates = [read, (path: string) => path === "tools/dependencies/assets.json" ? publishedAssets : read(path)]
    .map(reader => externalArtifacts(reader).find(item => item.name === "external-mise-amd64")!);
  const artifact = selectMise(candidates);
  const directory = mkdtempSync(join(tmpdir(), "nylzo-renovate-tools-"));
  const binary = await prepareExternal(artifact, directory);
  // A private mount leaves Containerbase's native tool installation writable.
  chmodSync(directory, 0o755);
  chmodSync(binary, 0o555);
  appendFileSync(process.env.GITHUB_OUTPUT!, `mise=${binary}\nversion=${artifact.version.replace(/^v/u, "")}\n`);
}
