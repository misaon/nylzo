import assert from "node:assert/strict";

export const repository = "misaon/nylzo";
export const dependencyFiles = new Set([
  "go.mod", "go.sum", "web/package.json", "web/core/package.json",
  "web/app/package.json", "web/e2e/package.json", "web/pnpm-lock.yaml",
]);

export function stableVersion(value: string): number[] {
  assert.match(value, /^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/u);
  const parts = value.replace(/^v/u, "").split(".").map(Number);
  assert(parts.every(Number.isSafeInteger));
  return parts;
}

export function compareVersions(first: string, second: string): number {
  const left = stableVersion(first), right = stableVersion(second);
  for (let index = 0; index < 3; index++) if (left[index] !== right[index]) return Math.sign(left[index]! - right[index]!);
  return 0;
}

export function metadata(pull: any, commits: any[], files: any[]) {
  assert.equal(pull.user?.login, "dependabot[bot]");
  assert.equal(pull.user?.id, 49699333);
  assert.equal(pull.head?.repo?.full_name, repository);
  assert.equal(pull.base?.repo?.full_name, repository);
  assert.equal(pull.base?.ref, "main");
  assert.equal(pull.draft, false);
  assert.equal(commits.length, 1, "Grouped or edited updates need maintainer review");
  const commit = commits[0];
  assert.equal(commit.sha, pull.head.sha);
  assert.equal(commit.author?.id, 49699333);
  assert.equal(commit.author?.login, "dependabot[bot]");
  assert.equal(commit.commit?.verification?.verified, true);
  assert.equal(commit.parents?.length, 1);
  assert(files.length > 0 && files.length <= dependencyFiles.size);
  for (const file of files) {
    assert(dependencyFiles.has(file.filename), `Unexpected update path: ${file.filename}`);
    assert.equal(file.status, "modified");
    assert(!file.previous_filename);
  }
  const message = commit.commit.message;
  const names = [...message.matchAll(/^\s*-?\s*dependency-name:\s*['"]?([^'"\s]+)['"]?\s*$/gmu)];
  assert.equal(names.length, 1, "Exactly one named dependency is required");
  const name = names[0]![1]!;
  assert.match(name, /^(?:@[a-zA-Z0-9._-]+\/)?[a-zA-Z0-9][a-zA-Z0-9./_~-]*$/u);
  const versions = message.match(/^Bumps .+ from (v?\d+\.\d+\.\d+) to (v?\d+\.\d+\.\d+)\.$/mu);
  assert(versions, "Unambiguous stable versions are required in the signed commit");
  const previous = versions[1]!, next = versions[2]!;
  const oldParts = stableVersion(previous), newParts = stableVersion(next);
  assert(compareVersions(next, previous) > 0);
  assert.equal(newParts[0], oldParts[0], "Major upgrades need maintainer review");
  if (oldParts[0] === 0) assert.equal(newParts[1], oldParts[1], "Pre-1.0 minor upgrades need maintainer review");
  const ecosystem = pull.head.ref.startsWith("dependabot/go_modules/") ? "go" :
    pull.head.ref.startsWith("dependabot/npm_and_yarn/") ? "npm" : "";
  assert(ecosystem, "Only Go and npm updates are eligible");
  assert(files.every(file => ecosystem === "go" ? file.filename.startsWith("go.") : file.filename.startsWith("web/")));
  return { name, previous, next, ecosystem, base: commit.parents[0].sha, head: commit.sha, pull: pull.number };
}

export function validateManifest(path: string, before: string, after: string, update: ReturnType<typeof metadata>) {
  assert.notEqual(before, after);
  if (path.endsWith("package.json")) {
    const previous = JSON.parse(before), next = JSON.parse(after);
    for (const section of ["dependencies", "devDependencies", "optionalDependencies", "peerDependencies"]) {
      if (previous[section]?.[update.name] !== undefined) {
        assert.equal(previous[section][update.name], update.previous);
        assert.equal(next[section]?.[update.name], update.next);
        next[section][update.name] = previous[section][update.name];
      }
    }
    assert.deepEqual(next, previous, "Only the named dependency version may change in a manifest");
  } else if (path === "go.mod") {
    const parse = (source: string) => {
      const versions = new Map<string,string>();
      const rest = source.replace(/^(\s*(?:require\s+)?)([A-Za-z0-9./_~+-]+)\s+(v\d+\.\d+\.\d+)(\s*(?:\/\/ indirect)?\s*)$/gmu,
        (_line, prefix, name, version, suffix) => { versions.set(name, version); return `${prefix}${name} VERSION${suffix}`; });
      return { rest, versions };
    };
    const left = parse(before), right = parse(after);
    assert.equal(right.rest, left.rest, "Go directives, replacements, and dependency names may not change");
    assert.equal(left.versions.get(update.name), update.previous);
    assert.equal(right.versions.get(update.name), update.next);
    for (const [name, version] of right.versions) if (version !== left.versions.get(name)) {
      const old = left.versions.get(name)!;
      assert(compareVersions(version, old) > 0);
      const older = stableVersion(old), newer = stableVersion(version);
      assert.equal(newer[0], older[0]);
      if (older[0] === 0) assert.equal(newer[1], older[1]);
    }
  } else if (path === "web/pnpm-lock.yaml") {
    assert.equal(before.split(/\n(?:importers|packages):\n/u)[0], after.split(/\n(?:importers|packages):\n/u)[0], "Lockfile security settings may not change");
    assert(!/^\s*(?:tarball|repo|commit):/mu.test(after), "Custom package sources need maintainer review");
    const escaped = update.name.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
    const contains = (text: string, version: string) => new RegExp(`^  ['\"]?${escaped}@${version.replaceAll(".", "\\.")}['\"]?:`, "mu").test(text);
    assert(contains(before, update.previous) && contains(after, update.next), "Resolved versions must match the signed update");
  } else assert.equal(path, "go.sum");
}

export function fixedAdvisories(update: ReturnType<typeof metadata>, before: any[], after: any[]) {
  const stillAffected = new Set(after.map(advisory => advisory.ghsa_id));
  return before.filter(advisory => !advisory.withdrawn_at && !stillAffected.has(advisory.ghsa_id) &&
    advisory.vulnerabilities?.some((vulnerability: any) =>
      vulnerability.package?.name === update.name && vulnerability.package?.ecosystem === update.ecosystem &&
      vulnerability.first_patched_version &&
      compareVersions(update.previous, vulnerability.first_patched_version) < 0 &&
      compareVersions(update.next, vulnerability.first_patched_version) >= 0)).map(advisory => advisory.ghsa_id);
}

export function nextPatch(tag: string) {
  assert.match(tag, /^v\d+\.\d+\.\d+$/u);
  const [major, minor, patch] = stableVersion(tag);
  assert(Number.isSafeInteger(patch! + 1));
  return `v${major}.${minor}.${patch! + 1}`;
}
