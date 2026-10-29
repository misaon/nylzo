import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { pages, repository } from "./github.mts";
import { versionParts } from "./policy.mts";

export function lastMainRelease() {
  const releases = pages(`repos/${repository}/releases?per_page=100`).filter(release => release.immutable && !release.draft && !release.prerelease && /^v\d+\.\d+\.\d+$/u.test(release.tag_name));
  assert(releases.length > 0 && releases.length <= 1000);
  releases.sort((left, right) => {
    const a = versionParts(left.tag_name), b = versionParts(right.tag_name);
    for (let index = 0; index < 3; index++) if (a[index] !== b[index]) return b[index]! - a[index]!;
    return 0;
  });
  for (const release of releases) {
    const result = spawnSync("git", ["rev-parse", `${release.tag_name}^{commit}`], { encoding: "utf8" });
    if (result.status !== 0) continue;
    const sha = result.stdout.trim(); assert.match(sha, /^[a-f0-9]{40}$/u);
    if (spawnSync("git", ["merge-base", "--is-ancestor", sha, "HEAD"]).status === 0) return { tag: release.tag_name as string, sha };
  }
  throw new Error("No published release is an ancestor of main");
}

export function changesAfter(sha: string): string[] {
  assert.match(sha, /^[a-f0-9]{40}$/u);
  return execFileSync("git", ["log", "--format=%B%x00", `${sha}..HEAD`], { encoding: "utf8" }).split("\0").map(message => message.trim()).filter(Boolean);
}
