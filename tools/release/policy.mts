import assert from "node:assert/strict";

export const automationIdentity = { login: "nylzo-security-automation[bot]", id: 348682855 };
export const semantic = /^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/u;

export function versionParts(version: string): number[] {
  assert.match(version, semantic);
  const parts = version.replace(/^v/u, "").split(".").map(Number);
  assert(parts.every(Number.isSafeInteger));
  return parts;
}

export function nextVersion(latest: string, commits: string[]): string | undefined {
  let bump = 0;
  for (const message of commits) {
    const header = message.split("\n")[0]!;
    const match = /^(feat|fix|perf|security|docs|refactor|build|ci|chore|test|style|revert)(?:\([^\r\n()]+\))?(!)?: .+/u.exec(header);
    if (!match || /^chore\(main\): release /u.test(header)) continue;
    bump = Math.max(bump, match[2] || /^BREAKING[ -]CHANGE: .+/mu.test(message) ? 3 : match[1] === "feat" ? 2 : 1);
  }
  if (!bump) return undefined;
  const [major, minor, patch] = versionParts(latest) as [number, number, number];
  const next = bump === 3 ? [major + 1, 0, 0] : bump === 2 ? [major, minor + 1, 0] : [major, minor, patch + 1];
  assert(next.every(Number.isSafeInteger));
  return next.join(".");
}

export function releaseBranch(tag: string): string {
  assert.match(tag, /^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$/u);
  return `release/${tag}`;
}

export function trustedBot(user: any): boolean {
  return user?.login === automationIdentity.login && user?.id === automationIdentity.id;
}

export function releaseMetadata(pull: any, files: any[], manifest: string): string {
  assert(trustedDependencyBot(pull.user), "Release PR must belong to the proposal-only App");
  assert.equal(pull.head.repo.full_name, "misaon/nylzo");
  assert.equal(pull.base.repo.full_name, "misaon/nylzo");
  assert.equal(pull.base.ref, "main");
  assert.equal(pull.head.ref, "release-please--branches--main");
  assert.equal(pull.merged, true);
  assert(files.length > 0 && files.length <= 2);
  assert(files.every(file => [".release-please-manifest.json", ".github/CHANGELOG.md"].includes(file.filename) && ["modified", "added"].includes(file.status) && !file.previous_filename));
  assert(files.some(file => file.filename === ".release-please-manifest.json"));
  const record = JSON.parse(manifest);
  assert.deepEqual(Object.keys(record), ["."]);
  versionParts(record["."]);
  assert.equal(pull.title, `chore(main): release ${record["."]}`);
  return `v${record["."]}`;
}

export function trustedDependencyBot(user: any): boolean {
  const id = process.env.DEPENDENCY_BOT_ID;
  return process.env.DEPENDENCY_APP_SLUG === "nylzo-dependency-updates" &&
    typeof id === "string" && /^[1-9][0-9]*$/u.test(id) && Number.isSafeInteger(Number(id)) &&
    user?.login === "nylzo-dependency-updates[bot]" && user?.id === Number(id);
}
