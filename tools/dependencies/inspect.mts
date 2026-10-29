import assert from "node:assert/strict";
import { trustedDependencyBot, releaseBranch } from "../release/policy.mts";
import { contents, pages, repository, request } from "../release/github.mts";
import { validateDependencyFile } from "./policy.mts";

export class UpdateDeferred extends Error {}

export function inspectDependencyPull(number: number, merged = false, latest?: any) {
  assert(Number.isSafeInteger(number) && number > 0);
  const pull = request(`repos/${repository}/pulls/${number}`);
  assert(trustedDependencyBot(pull.user), "Only the repository App can propose automatic updates");
  assert.equal(pull.head.repo.full_name, repository); assert.equal(pull.base.repo.full_name, repository);
  assert.equal(pull.merged, merged); assert.equal(pull.draft, false);
  assert.match(pull.head.ref, /^renovate\/[a-zA-Z0-9_./-]+$/u);
  if (pull.base.ref !== "main" && pull.base.ref !== releaseBranch((latest ?? request(`repos/${repository}/releases/latest`)).tag_name)) throw new UpdateDeferred("This proposal targets a superseded release");
  const commits = pages(`repos/${repository}/pulls/${number}/commits?per_page=100`);
  assert(commits.length === 1 && commits[0].sha === pull.head.sha, "Edited histories require attention");
  const commit = commits[0];
  assert(trustedDependencyBot(commit.author)); assert.equal(commit.commit.verification.verified, true); assert.equal(commit.parents.length, 1);
  const base = commit.parents[0].sha;
  if (!merged) {
    if (pull.base.sha !== request(`repos/${repository}/git/ref/heads/${pull.base.ref}`).object.sha || base !== pull.base.sha) throw new UpdateDeferred("Rebase and retest on the current base");
  }
  const files = pages(`repos/${repository}/pulls/${number}/files?per_page=100`);
  assert.equal(files.length, pull.changed_files); assert(files.length > 0 && files.length <= 80);
  const changes = [];
  for (const file of files) {
    assert.equal(file.status, "modified"); assert(!file.previous_filename);
    const before = contents(file.filename, base), after = contents(file.filename, pull.head.sha);
    changes.push(...validateDependencyFile(file.filename, before, after));
    if (file.filename === "tools/dependencies/assets.json") {
      const previous = JSON.parse(before), next = JSON.parse(after);
      for (const [key, value] of Object.entries(next) as [string,any][]) {
        if (JSON.stringify(previous[key]) === JSON.stringify(value)) continue;
        const architecture = key.endsWith("-amd64") ? "amd64" : "arm64";
        const version = value.version.replace(/^(?:v|jq-)/u, "");
        const name = key.startsWith("gh-") ? `gh_${version}_linux_${architecture}.tar.gz` :
          key.startsWith("cosign-") ? `cosign-linux-${architecture}` :
          key.startsWith("jq-") ? `jq-linux-${architecture}` :
          `ignition-validate-${architecture === "amd64" ? "x86_64" : "aarch64"}-linux`;
        const release = request(`repos/${value.package}/releases/tags/${encodeURIComponent(value.version)}`);
        const asset = release.assets.find((item: any) => item.name === name);
        assert(asset && asset.digest === `sha256:${value.sha256}`, "Binary checksum must match GitHub's upstream release asset digest");
      }
    }
    if (file.filename === "deploy/hetzner-flatcar/toolchain.json") {
      const assets = JSON.parse(contents("tools/dependencies/assets.json", pull.head.sha)), next = JSON.parse(after);
      for (const name of ["gh", "cosign", "jq"]) {
        assert.equal(next[name].version, assets[`${name}-amd64`].version.replace(/^(?:v|jq-)/u, ""));
        assert.equal(next[name].version, assets[`${name}-arm64`].version.replace(/^(?:v|jq-)/u, ""));
        for (const architecture of ["amd64", "arm64"]) assert.equal(next[name][architecture], assets[`${name}-${architecture}`].sha256);
      }
    }
    if (/^\.github\/workflows\//u.test(file.filename)) {
      const oldPins = new Set([...before.matchAll(/uses: ([A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)(?:\/[^@\s]+)?@([a-f0-9]{40}) # (v?\d+\.\d+\.\d+)/gu)].map(match => `${match[1]}@${match[2]}`));
      for (const pin of after.matchAll(/uses: ([A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)(?:\/[^@\s]+)?@([a-f0-9]{40}) # (v?\d+\.\d+\.\d+)/gu)) {
        if (oldPins.has(`${pin[1]}@${pin[2]}`)) continue;
        let object = request(`repos/${pin[1]}/git/ref/tags/${pin[3]}`).object;
        for (let depth = 0; object.type === "tag" && depth < 3; depth++) object = request(`repos/${pin[1]}/git/tags/${object.sha}`).object;
        assert.equal(object.type, "commit"); assert.equal(object.sha, pin[2], "Action SHA must match the actual version tag in its upstream repository");
      }
    }
  }
  assert(changes.length > 0 || files.every(file => /(?:lock|go\.sum)/u.test(file.filename)), "No dependency change found");
  return { pull, base, changes, files };
}

export function requireAttention(pull: any, reason: string): void {
  assert(trustedDependencyBot(pull.user) && pull.head.repo.full_name === repository);
  request(`repos/${repository}/issues/${pull.number}/labels`, "POST", { labels: ["requires-attention"] });
  request(`repos/${repository}/issues/${pull.number}/assignees`, "POST", { assignees: ["misaon"] });
  console.log(`PR #${pull.number} requires maintainer attention: ${reason}`);
}
