import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fixedAdvisories, metadata, repository, validateManifest } from "./dependency-policy.mts";

export function api(path: string, body?: object): any {
  const arguments_ = ["api", "--hostname", "github.com", path];
  if (body) arguments_.push("--method", "PUT", "--input", "-");
  return JSON.parse(execFileSync("gh", arguments_, {
    encoding: "utf8", maxBuffer: 16 * 1024 * 1024,
    ...(body ? { input: JSON.stringify(body) } : {}),
  }));
}

export function list(path: string): any[] {
  return JSON.parse(execFileSync("gh", ["api", "--hostname", "github.com", "--paginate", "--slurp", path], {
    encoding: "utf8", maxBuffer: 16 * 1024 * 1024,
  })).flat();
}

export function content(path: string, reference: string): string {
  assert.match(reference, /^[0-9a-f]{40}$/u);
  const result = api(`repos/${repository}/contents/${path}?ref=${reference}`);
  assert.equal(result.encoding, "base64");
  assert.equal(result.type, "file");
  return Buffer.from(result.content, "base64").toString("utf8");
}

export function inspectUpdate(number: number, merged = false) {
  assert(Number.isSafeInteger(number) && number > 0);
  const pull = api(`repos/${repository}/pulls/${number}`);
  assert.equal(pull.merged, merged);
  assert.equal(pull.state, merged ? "closed" : "open");
  const commits = list(`repos/${repository}/pulls/${number}/commits?per_page=100`);
  const files = list(`repos/${repository}/pulls/${number}/files?per_page=100`);
  assert.equal(files.length, pull.changed_files, "Truncated file list");
  const update = metadata(pull, commits, files);
  if (!merged) {
    assert.equal(pull.base.sha, api(`repos/${repository}/git/ref/heads/main`).object.sha, "Update must be current with main");
    assert.equal(update.base, pull.base.sha);
  }
  for (const file of files) validateManifest(file.filename, content(file.filename, update.base), content(file.filename, update.head), update);
  const advisories = (version: string) => list(`advisories?type=reviewed&ecosystem=${update.ecosystem}&affects=${encodeURIComponent(`${update.name}@${version}`)}&per_page=100`);
  const fixed = fixedAdvisories(update, advisories(update.previous), advisories(update.next));
  assert(fixed.length > 0, "No reviewed advisory is fixed by this update");
  return { ...update, fixed, mergedCommit: pull.merge_commit_sha };
}
