import assert from "node:assert/strict";
import test from "node:test";
import { createRequire } from "node:module";
import batches from "./batches.cjs";

const source = "a".repeat(40), head = "b".repeat(40);
const candidate = (name: string, version = "1.2.4", digest?: string) => ({ name, manager: "npm", datasource: "npm", value: version, version, ...(digest ? { digest } : {}) });
const items = [candidate("first"), candidate("second"), candidate("third")];
const metadata = (candidates = items, phase = "batch", root = 0) => ({ base: "release/v0.1.6", phase, mode: "security", root, head: root ? head : "", candidates });
const root = () => ({ number: 10, head, base: source, labels: ["automation:batch-failed", "automation:ci-failed"], batch: metadata(), terminal: "fail" });
const diagnostic = (item: any, terminal?: string) => ({ number: 20 + items.indexOf(item), head: "c".repeat(40), base: source, labels: [], batch: metadata([item], "diagnostic", 10), terminal });
const plan = (records: any[]) => batches.planIsolation(records, "release/v0.1.6", source, "security");

test("signed metadata is bounded data; malformed phases, commands and duplicate footers cannot become a plan", () => {
  const footer = (value: any) => `fix(deps): update\n\nNylzo-Batch-V1: ${JSON.stringify(value)}`;
  assert.deepEqual(batches.readBatch(footer(metadata())), metadata());
  assert.equal(batches.readBatch("fix(deps): legacy update"), null);
  const repeated = batches.readBatch(footer(metadata(Array.from({ length: 150 }, () => items[0]!))));
  assert.equal(repeated.candidates.length, 1, "One Action repeated across workflow files remains one update target");
  assert.throws(() => batches.readBatch(footer(metadata(Array.from({ length: 129 }, (_, index) => candidate(`package-${index}`))))));
  for (const value of [{ ...metadata(), phase: "publish" }, { ...metadata(), root: 3 }, { ...metadata(), candidates: [candidate("$(curl attacker)")] }, { ...metadata(), command: "sh" }]) assert.throws(() => batches.readBatch(footer(value)));
  assert.throws(() => batches.readBatch(footer(metadata()) + "\n" + footer(metadata())));
});

test("a failed three-update batch diagnoses every unit, waits for all results and regroups only the two passing targets", () => {
  const records = [root()];
  assert.equal(plan(records).plan.phase, "diagnostic");
  records.push(diagnostic(items[0], "pass"), diagnostic(items[1], "fail"), diagnostic(items[2]));
  assert.equal(plan(records).plan.phase, "diagnostic", "A missing result cannot silently exclude an update");
  records.at(-1)!.terminal = "pass";
  const result = plan(records);
  assert.equal(result.plan.phase, "regroup");
  assert(batches.same(result.plan.candidates, [items[0], items[2]]));
  assert(batches.same(result.excluded, [items[1]]));
  const newer = candidate("second", "1.2.5");
  assert.notEqual(batches.key(newer), batches.key(result.excluded[0]), "A newer upstream fix is not quarantined");
});

test("diagnostic evidence from a different root head, baseline or unit cannot authorize regrouping", () => {
  for (const mutation of [
    (record: any) => record.batch.head = "f".repeat(40),
    (record: any) => record.base = "f".repeat(40),
    (record: any) => record.batch.candidates.push(candidate("unrelated")),
  ]) {
    const record = diagnostic(items[0], "pass"); mutation(record);
    assert.equal(plan([root(), record, diagnostic(items[1], "pass"), diagnostic(items[2], "pass")]).plan.phase, "diagnostic");
  }
  const held = root(); held.labels.push("manual-hold");
  assert.equal(plan([held]).plan, undefined);
});

test("related tool pins stay atomic and a digest quarantine distinguishes a repaired image", () => {
  assert.equal(batches.unitId("caddy"), batches.unitId("caddyserver/caddy"));
  assert.equal(batches.unitId("go"), batches.unitId("golang/go"));
  assert.equal(batches.units([candidate("go"), candidate("golang/go")]).size, 1);
  assert.notEqual(batches.key(candidate("image", "1.2.4", "sha256:" + "a".repeat(64))), batches.key(candidate("image", "1.2.4", "sha256:" + "b".repeat(64))));
});

test("interaction failures shrink recombinations deterministically and never repeat or issue individual releases", () => {
  const records = [root(), ...items.map(item => diagnostic(item, "pass"))];
  const attempted = new Set();
  for (let count = items.length; count > 0; count--) {
    const result = plan(records).plan;
    assert.deepEqual(plan(records).excluded, [], "A failed combination cannot quarantine individually passing members globally");
    assert.equal(result.phase, "regroup");
    assert.equal(batches.units(result.candidates).size, count);
    const id = batches.setId(result.candidates); assert(!attempted.has(id)); attempted.add(id);
    records.push({ number: 50 + count, head, base: source, labels: ["automation:ci-failed"], batch: metadata(result.candidates, "regroup", 10), terminal: "fail" });
  }
  assert.equal(plan(records).plan, undefined, "No infinite retry loop when even the final combination fails");
});

test("an unavailable immutable target does not stall already verified updates", () => {
  const failed = { ...root(), unavailable: [batches.unitId(items[1]!.name)] };
  const result = plan([failed, diagnostic(items[0], "pass"), diagnostic(items[2], "pass")]);
  assert.equal(result.plan.phase, "regroup");
  assert(batches.same(result.plan.candidates, [items[0], items[2]]));
  assert.deepEqual(result.excluded, [{ ...items[1], base: "release/v0.1.6" }]);
});

test("Renovate default and prefixed recovery branches have the same phase", () => {
  for (const prefix of ["renovate/", "renovate/main-", "renovate/release/v0.1.6-"]) {
    assert.equal(batches.branchPhase(`${prefix}diagnose-10-012345abcdef`), "diagnostic");
    assert.equal(batches.diagnosticBranch(`${prefix}diagnose-10-012345abcdef`), true);
    assert.equal(batches.branchPhase(`${prefix}regroup-10-012345abcdef`), "regroup");
  }
  for (const ref of ["feature/diagnose-10-012345abcdef", "renovate/diagnose-10-short", "renovate/main-typescript-7.x"]) assert.equal(batches.branchPhase(ref), null);
});

test("actual signed diagnostic evidence supersedes an obsolete unavailable marker", () => {
  const failed = { ...root(), unavailable: items.map(item => batches.unitId(item.name)) };
  const result = plan([failed, ...items.map(item => diagnostic(item, "pass"))]);
  assert.equal(result.plan.phase, "regroup");
  assert(batches.same(result.plan.candidates, items));
  assert.deepEqual(result.excluded, [], "An old lookup marker cannot veto a subsequently verified update");
});

test("a partial tool diagnostic cannot quarantine or approve an atomic multi-pin update", () => {
  const native = { name: "goreleaser", manager: "mise", datasource: "github-tags", value: "2.19.0", version: "2.19.0" };
  const action = { name: "goreleaser/goreleaser", manager: "regex", datasource: "github-releases", value: "v2.19.0", version: "v2.19.0" };
  const failed = { ...root(), batch: metadata([native, action, items[2]]), unavailable: [batches.unitId("goreleaser")] };
  const partial = { ...diagnostic(action, "fail"), batch: metadata([action], "diagnostic", 10) };
  const records = [failed, partial, diagnostic(items[2], "pass")];
  const result = plan(records);
  assert.equal(result.plan.phase, "diagnostic");
  assert(batches.same(result.plan.candidates, [native, action]));
  assert.deepEqual(result.excluded, [], "A broken one-sided pin update is not evidence of an upstream regression");
  partial.terminal = "pass";
  assert.equal(plan(records).plan.phase, "diagnostic", "A partial success cannot approve the complete tool either");
  partial.terminal = "fail";
  partial.batch.candidates = [native, action];
  assert.equal(plan(records).plan.phase, "regroup");
  assert(batches.same(plan(records).excluded, [native, action]), "A complete failed diagnostic retains quarantine");
});

test("lookup absence cannot leak across main, production, modes or changed source commits", () => {
  const failed = { ...root(), unavailable: [batches.unitId(items[1]!.name)] };
  const records = [failed, diagnostic(items[0], "pass"), diagnostic(items[2], "pass")];
  for (const [base, sha, mode] of [["main", source, "security"], ["release/v0.1.6", "f".repeat(40), "security"], ["release/v0.1.6", source, "weekly"]]) {
    assert.deepEqual(batches.planIsolation(records, base, sha, mode).excluded, []);
  }
});

test("retirement preserves current regroup proof, failed targets, changed human holds and unavailable roots", () => {
  const first = diagnostic(items[0], "pass"), second = diagnostic(items[1], "fail"), third = diagnostic(items[2], "pass");
  assert.deepEqual(batches.staleEvidence([root(), first, second, third], { "release/v0.1.6": source }), [], "Current diagnostics are needed by verifyRegroup");
  const stale = batches.staleEvidence([root(), first, second, third], { main: "f".repeat(40) });
  assert.deepEqual(stale.map(item => item.number), [10, first.number, third.number]);
  const held = { ...first, labels: ["manual-hold"] }, human = { ...third, labels: ["requires-attention"] };
  assert.deepEqual(batches.staleEvidence([held, human, second], {}), []);
  const unavailable = { ...root(), unavailable: [batches.unitId(items[2]!.name)] };
  assert.deepEqual(batches.staleEvidence([unavailable, diagnostic(items[0], "fail"), second], { "release/v0.1.6": source }), [], "An unavailable target still requires its root evidence");
});

test("exhausted roots and stale duplicate failures retire without losing exact-target quarantine", () => {
  const failed = items.map(item => diagnostic(item, "fail"));
  assert.deepEqual(batches.staleEvidence([root(), ...failed], { "release/v0.1.6": source }).map(item => item.number), [10]);
  const duplicate = { ...failed[0], number: 30, base: "f".repeat(40) };
  assert.deepEqual(batches.staleEvidence([failed[0], duplicate], { "release/v0.1.6": "f".repeat(40) }).map(item => item.number), [failed[0]!.number]);
  assert(batches.same(plan([duplicate]).excluded, [items[0]]));
});

test("completed diagnostic evidence never inflates the two unfinished-proposal slots", () => {
  const require = createRequire(import.meta.url), path = require.resolve("../../.github/renovate.cjs");
  const regexTarget = { name: "jdx/mise", manager: "regex", datasource: "github-release-attachments", value: "v2026.10.20", version: "v2026.10.20" };
  const native = { name: "goreleaser", manager: "mise", datasource: "github-tags", value: "2.19.0", version: "2.19.0" };
  const values = { NYLZO_UPDATE_MODE: "weekly", NYLZO_MISE_VERSION: "2026.10.19", NYLZO_BASE_TAG: "v0.1.6", NYLZO_BLOCKED_PROPOSALS: "40", NYLZO_BATCH_PLAN: JSON.stringify({ plans: { "release/v0.1.6": { phase: "diagnostic", root: 10, head, candidates: [items[0], regexTarget, native] } }, excluded: [{ ...items[1], base: "main" }] }) };
  const previous = Object.fromEntries(Object.keys(values).map(name => [name, process.env[name]]));
  try {
    Object.assign(process.env, values); delete require.cache[path];
    assert.equal(require(path).prConcurrentLimit, 2);
    assert.deepEqual(require(path).force.packageRules.find((rule: any) => rule.enabled === false && rule.matchJsonata)?.matchBaseBranches, ["main"], "Unavailable main targets cannot disable a production target");
    assert.deepEqual(require(path).force.packageRules.findLast((rule: any) => rule.enabled && rule.matchPackageNames?.includes("jdx/mise")).matchManagers, ["custom.regex"], "Signed metadata uses the internal regex name; selectors require custom.regex");
    const allowed = require(path).force.packageRules.findLast((rule: any) => rule.enabled && rule.matchPackageNames?.includes("goreleaser")).allowedVersions;
    const pattern = new RegExp(allowed.slice(1, -1));
    assert(pattern.test("v2.19.0") && pattern.test("2.19.0"), "Raw GitHub tags and normalized Mise versions describe the same snapshot");
    assert(!pattern.test("v2.19.1") && !pattern.test("2.19.1"));
    process.env.NYLZO_BATCH_PLAN = '{"plans":{},"excluded":[]}'; delete require.cache[path];
    assert.equal(require(path).prConcurrentLimit, 42, "Ordinary migration holds retain their existing budget policy");
  } finally {
    delete require.cache[path];
    for (const [name, value] of Object.entries(previous)) if (value === undefined) delete process.env[name]; else process.env[name] = value;
  }
});
