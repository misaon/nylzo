import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { writeFileSync } from "node:fs";

const tag = process.env.GITHUB_REF_NAME!;
assert.match(tag, /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/u);
const object = execFileSync("git", ["cat-file", "tag", `refs/tags/${tag}`], { encoding: "utf8" });
const values = (name: string) => [...object.matchAll(new RegExp(`^${name}: ([^\\n]+)$`, "gmu"))].map(match => match[1]!);
const kinds = values("Release-Kind"), policies = values("Policy-Commit");
assert(kinds.length <= 1 && policies.length <= 1);
const kind = kinds[0] ?? "manual";
assert(["planned", "security", "weekly", "manual"].includes(kind));
if (kind !== "manual") assert(policies.length === 1 && /^[a-f0-9]{40}$/u.test(policies[0]!));
assert.match(process.env.GITHUB_SHA!, /^[a-f0-9]{40}$/u);
writeFileSync(process.argv[2]!, JSON.stringify({ format: 1, tag, source: process.env.GITHUB_SHA, kind, policyCommit: policies[0] ?? null }, null, 2) + "\n");
