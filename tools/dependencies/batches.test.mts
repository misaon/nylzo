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
  assert(batches.same(result.excluded, [items[1]]));
});

test("completed diagnostic evidence never inflates the two unfinished-proposal slots", () => {
  const require = createRequire(import.meta.url), path = require.resolve("../../.github/renovate.cjs");
  const values = { NYLZO_UPDATE_MODE: "weekly", NYLZO_MISE_VERSION: "2026.10.19", NYLZO_BASE_TAG: "v0.1.6", NYLZO_BLOCKED_PROPOSALS: "40", NYLZO_BATCH_PLAN: JSON.stringify({ plans: { "release/v0.1.6": { phase: "diagnostic", root: 10, head, candidates: [items[0]] } }, excluded: [] }) };
  const previous = Object.fromEntries(Object.keys(values).map(name => [name, process.env[name]]));
  try {
    Object.assign(process.env, values); delete require.cache[path];
    assert.equal(require(path).prConcurrentLimit, 2);
    process.env.NYLZO_BATCH_PLAN = '{"plans":{},"excluded":[]}'; delete require.cache[path];
    assert.equal(require(path).prConcurrentLimit, 42, "Ordinary migration holds retain their existing budget policy");
  } finally {
    delete require.cache[path];
    for (const [name, value] of Object.entries(previous)) if (value === undefined) delete process.env[name]; else process.env[name] = value;
  }
});
