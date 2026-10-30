// Shared by the isolated coordinator and protected-main Renovate config.
const assert = require("node:assert/strict");
const { createHash } = require("node:crypto");
const marker = "Nylzo-Batch-V1: ";
const aliases = [
  ["go", "golang/go"], ["node", "nodejs/node"], ["caddy", "caddyserver/caddy"],
  ["opentofu", "opentofu/opentofu"], ["mise", "jdx/mise"], ["gh", "cli/cli"],
  ["cosign", "sigstore/cosign"], ["jq", "jqlang/jq"], ["syft", "anchore/syft"],
  ["goreleaser", "goreleaser/goreleaser"], ["gitsign", "sigstore/gitsign"],
  ["codecov-cli", "codecov/codecov-cli"], ["ignition-validate", "coreos/ignition"],
];
const unit = name => aliases.find(names => names.includes(name))?.[0] || name;
const unitId = name => createHash("sha256").update(unit(name)).digest("hex").slice(0, 12);
const key = item => JSON.stringify([item.name, item.manager, item.datasource, item.value, item.version || "", item.digest || ""]);
const keys = items => items.map(key).sort();
const same = (left, right) => JSON.stringify(keys(left)) === JSON.stringify(keys(right));
const setId = items => createHash("sha256").update(JSON.stringify(keys(items))).digest("hex").slice(0, 12);
const diagnosticBranch = ref => /-diagnose-\d+-[a-f0-9]{12}$/u.test(ref);

function readBatch(message) {
  const lines = message.split("\n").filter(line => line.startsWith(marker));
  if (!lines.length) return null;
  assert.equal(lines.length, 1); assert(lines[0].length <= 65536);
  const value = JSON.parse(lines[0].slice(marker.length));
  assert.deepEqual(Object.keys(value).sort(), ["base", "candidates", "head", "mode", "phase", "root"]);
  assert(["batch", "diagnostic", "regroup"].includes(value.phase));
  assert(["security", "weekly"].includes(value.mode));
  assert(/^(?:main|release\/v\d+\.\d+\.\d+)$/u.test(value.base));
  assert(Number.isSafeInteger(value.root) && value.root >= 0);
  assert(value.phase === "batch" ? value.root === 0 && value.head === "" : value.root > 0 && /^[a-f0-9]{40}$/u.test(value.head));
  assert(Array.isArray(value.candidates) && value.candidates.length > 0 && value.candidates.length <= 1024);
  for (const item of value.candidates) {
    assert(Object.keys(item).every(name => ["name", "manager", "datasource", "value", "version", "digest"].includes(name)));
    for (const name of ["name", "manager", "datasource"]) assert(typeof item[name] === "string" && item[name].length > 0 && item[name].length <= 512 && !/[\x00-\x20\x7f]/u.test(item[name]));
    assert(typeof item.value === "string" && item.value.length > 0 && item.value.length <= 512 && !/[\x00-\x1f\x7f]/u.test(item.value));
    for (const name of ["version", "digest"]) assert(item[name] === undefined || (typeof item[name] === "string" && item[name].length <= 512 && !/[\x00-\x20\x7f]/u.test(item[name])));
    assert(/^[A-Za-z0-9@_./:+-]+$/u.test(item.name));
  }
  value.candidates = [...new Map(value.candidates.map(item => [key(item), item])).values()].sort((a, b) => key(a).localeCompare(key(b)));
  assert(value.candidates.length <= 128, "A batch is limited to 128 distinct update targets");
  return value;
}

function template(mode, phase = "batch", root = 0, head = "") {
  const fields = '"name" depName "manager" manager "datasource" datasource "value" newValue "version" newVersion "digest" newDigest';
  return `${marker}{"base":{{{toJSON baseBranch}}},"phase":${JSON.stringify(phase)},"mode":${JSON.stringify(mode)},"root":${root},"head":${JSON.stringify(head)},"candidates":[{{#each upgrades}}{{{toJSON (toObject ${fields})}}}{{#unless @last}},{{/unless}}{{/each}}]}`;
}

function units(candidates) {
  const result = new Map();
  for (const candidate of candidates) {
    const id = unitId(candidate.name);
    result.set(id, [...(result.get(id) || []), candidate]);
  }
  return result;
}

function planIsolation(records, base, sha, mode) {
  const excluded = [];
  for (const record of records) {
    for (const id of record.unavailable || []) excluded.push(...record.batch.candidates.filter(item => unitId(item.name) === id));
    if (record.terminal === "fail" && (record.batch.phase !== "batch" || units(record.batch.candidates).size === 1 || record.labels.includes("manual-hold"))) excluded.push(...record.batch.candidates);
  }
  const roots = records.filter(record => record.base === sha && record.batch.base === base && record.batch.mode === mode && record.batch.phase === "batch" && record.terminal === "fail" && record.labels.includes("automation:batch-failed") && !record.labels.includes("manual-hold") && units(record.batch.candidates).size > 1).sort((a, b) => b.number - a.number);
  const root = roots[0];
  if (!root) return { excluded };
  const related = records.filter(record => record.base === sha && record.batch.root === root.number && record.batch.head === root.head && record.batch.base === base && record.batch.mode === mode);
  let passing = [];
  const pendingCandidates = [];
  let pending = false;
  for (const [id, candidates] of units(root.batch.candidates)) {
    const record = related.filter(record => record.batch.phase === "diagnostic" && same(record.batch.candidates, candidates)).sort((a, b) => b.number - a.number)[0];
    if (!record && root.unavailable?.includes(id)) continue;
    else if (!record?.terminal) { pending = true; pendingCandidates.push(...candidates); }
    else if (record.terminal === "pass") passing.push(...candidates);
  }
  // A passing individual update can still conflict with another. Each failed
  // recombination removes one deterministic unit, never repeating a tested
  // combination. At most N recombinations can follow N individual checks.
  if (!pending) {
    while (passing.length && related.some(record => record.batch.phase === "regroup" && record.terminal === "fail" && same(record.batch.candidates, passing))) {
      const last = [...units(passing).keys()].sort().at(-1);
      passing = passing.filter(item => unitId(item.name) !== last);
    }
  }
  if (!pending && !passing.length) return { excluded };
  return { excluded, plan: { phase: pending ? "diagnostic" : "regroup", root: root.number, head: root.head, candidates: pending ? pendingCandidates : passing } };
}

// Select exact snapshot targets after lookup as well as by package identity.
// The initial identity rule allows lookup; this late rule rejects any newer
// version/digest that appeared while the diagnostic run was being prepared.
function targetExpression(item) {
  const identity = `depName = ${JSON.stringify(item.name)} and manager = ${JSON.stringify(item.manager)} and datasource = ${JSON.stringify(item.datasource)}`;
  const target = [`newValue = ${JSON.stringify(item.value)}`];
  if (item.version) target.push(`newVersion = ${JSON.stringify(item.version)}`);
  if (item.digest) target.push(`newDigest = ${JSON.stringify(item.digest)}`);
  return { identity, target: target.join(" and ") };
}

module.exports = { diagnosticBranch, key, keys, same, setId, readBatch, template, units, unitId, planIsolation, targetExpression };
