// This global configuration is loaded only from protected main, never a PR.
const fs = require("node:fs");
const assert = require("node:assert/strict");
const batches = require(process.env.NYLZO_BATCH_HELPERS || "../tools/dependencies/batches.cjs");
const config = JSON.parse(fs.readFileSync(process.env.NYLZO_RENOVATE_CONFIG || "renovate.json", "utf8"));
const miseVersion = process.env.NYLZO_MISE_VERSION;
assert(/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(miseVersion));
const mode = process.env.NYLZO_UPDATE_MODE;
assert(["security", "weekly"].includes(mode));
const tag = process.env.NYLZO_BASE_TAG;
assert(/^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(tag));
const affected = JSON.parse(process.env.NYLZO_AFFECTED_DEPENDENCIES || "[]");
assert(Array.isArray(affected) && affected.every(name => typeof name === "string" && /^[A-Za-z0-9@_./+-]+$/.test(name)));
const blocked = Number(process.env.NYLZO_BLOCKED_PROPOSALS || "0");
assert(Number.isSafeInteger(blocked) && blocked >= 0 && blocked <= 1000);
const batchName = `${mode} dependency batch`;
const batchSlug = `${mode}-dependency-batch`;
// Advisory defaults also group major fixes. Keep those and pre-1.0 minor
// fixes on migration branches so they cannot block the nonbreaking batch.
const breakingAlert = '(or isMajor (and isMinor (or (equals newMajor 0) (equals newMajor "0"))))';
// One signed commit carries compatible updates across all enabled managers.
// Breaking upgrades retain their separate migration rules and production ban.
const batchRules = [
  { matchUpdateTypes: ["patch", "digest", "pin", "pinDigest"], groupName: batchName, groupSlug: batchSlug, groupSingleUpdates: true },
  { matchUpdateTypes: ["minor"], matchCurrentVersion: "!/^0\\./", groupName: batchName, groupSlug: batchSlug, groupSingleUpdates: true },
];
const isolation = JSON.parse(process.env.NYLZO_BATCH_PLAN || '{"plans":{},"excluded":[]}');
assert(isolation && typeof isolation.plans === "object" && Array.isArray(isolation.excluded));
const bases = [`release/${tag}`, "main"];
const isolationRules = [];
for (const exclusion of isolation.excluded) {
  const { base, ...item } = exclusion;
  assert(base === undefined || bases.includes(base));
  batches.readBatch(`Nylzo-Batch-V1: ${JSON.stringify({ base: "main", phase: "batch", mode, root: 0, head: "", candidates: [item] })}`);
  const expression = batches.targetExpression(item);
  isolationRules.push({ ...(base ? { matchBaseBranches: [base] } : {}), matchJsonata: [`$exists(newValue) and (${expression.identity}) and (${expression.target})`], enabled: false });
}
for (const [base, plan] of Object.entries(isolation.plans)) {
  assert(bases.includes(base));
  const snapshot = batches.readBatch(`Nylzo-Batch-V1: ${JSON.stringify({ ...plan, base, mode })}`);
  assert(["diagnostic", "regroup"].includes(snapshot.phase));
  isolationRules.push({ matchBaseBranches: [base], matchPackageNames: ["*"], enabled: false });
  const identities = new Map();
  for (const candidate of snapshot.candidates) {
    const identity = JSON.stringify([candidate.name, candidate.manager, candidate.datasource]);
    identities.set(identity, [...(identities.get(identity) || []), candidate]);
  }
  for (const candidates of identities.values()) {
    const item = candidates[0];
    const slug = plan.phase === "diagnostic" ? `diagnose-${plan.root}-${batches.unitId(item.name)}` : `regroup-${plan.root}-${batches.setId(snapshot.candidates)}`;
    const semver = /^v?\d+\.\d+\.\d+(?:[-+][A-Za-z0-9.+-]+)?$/u;
    const versions = [...new Set(candidates.map(candidate => candidate.version || (semver.test(candidate.value) ? candidate.value : undefined)).filter(Boolean))];
    // allowedVersions filters raw upstream tags before normalization. Mise
    // records 2.19.0 while GitHub returns v2.19.0. The late target rule below
    // still binds the exact normalized value, version and digest.
    const versionPattern = version => {
      const normalized = semver.test(version) ? version.replace(/^v/u, "") : version;
      return `${semver.test(version) ? "v?" : ""}${normalized.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`;
    };
    isolationRules.push({
      matchBaseBranches: [base], matchPackageNames: [item.name], matchManagers: [item.manager === "regex" ? "custom.regex" : item.manager], matchDatasources: [item.datasource], enabled: true,
      ...(versions.length ? { allowedVersions: `/^(?:${versions.map(versionPattern).join("|")})$/` } : {}),
      groupName: slug, groupSlug: slug, groupSingleUpdates: true,
      minimumReleaseAge: null,
      commitBody: batches.template(mode, snapshot.phase, snapshot.root, snapshot.head),
    });
    const identity = batches.targetExpression(item).identity;
    const targets = candidates.map(candidate => `(${batches.targetExpression(candidate).target})`).join(" or ");
    isolationRules.push({ matchBaseBranches: [base], matchJsonata: [`$exists(newValue) and (${identity}) and $not(${targets})`], enabled: false });
  }
}
const isolating = Object.keys(isolation.plans).length > 0;
module.exports = {
  ...config,
  // Current manual holds do not consume the two ordinary update slots.
  // Isolation generates only unfinished probes, so completed/held branches
  // are already outside Renovate's counter and need no slot compensation.
  prConcurrentLimit: isolating ? config.prConcurrentLimit : config.prConcurrentLimit + blocked,
  // Isolation has a bounded signed target list; prepare its branches without
  // letting an unavailable branch consume one of the two actual PR/CI slots.
  branchConcurrentLimit: isolating ? 0 : null,
  platform: "github",
  repositories: ["misaon/nylzo"],
  autodiscover: false,
  onboarding: false,
  requireConfig: "ignored",
  baseBranchPatterns: isolating ? Object.keys(isolation.plans) : bases,
  useBaseBranchConfig: "none",
  forkProcessing: "disabled",
  allowScripts: false,
  allowPlugins: false,
  exposeAllEnv: false,
  allowedUnsafeExecutions: [],
  allowedCommands: ["^node /opt/nylzo-dependency-sync\\.mts --locks$"],
  allowShellExecutorForPostUpgradeCommands: false,
  cacheDir: "/tmp/renovate-cache",
  baseDir: "/tmp/renovate-work",
  persistRepoData: false,
  // Preserve signed failed snapshots and diagnostic evidence across runs.
  pruneStaleBranches: false,
  commitBody: batches.template(mode),
  ...(isolating ? { osvVulnerabilityAlerts: false } : {}),
  // Native advisory remediations otherwise reset grouping and branchTopic.
  vulnerabilityAlerts: {
    ...config.vulnerabilityAlerts,
    enabled: !isolating,
    groupName: batchName,
    groupSlug: `{{#if ${breakingAlert}}}migration-{{depNameSanitized}}-{{newMajor}}.{{newMinor}}{{else}}${batchSlug}{{/if}}`,
    branchTopic: batchSlug,
    groupSingleUpdates: true,
    labels: [...new Set(["dependencies", `automation:${mode}`, "automation:security"])],
  },
  force: {
    constraints: { mise: miseVersion },
    ignoreScripts: true,
    ignorePlugins: true,
    automerge: false,
    separateMinorPatch: false,
    platformCommit: "enabled",
    schedule: [],
    labels: ["dependencies", `automation:${mode}`],
    packageRules: [...(mode === "security" ? [
      ...config.packageRules,
      { matchPackageNames: ["*"], enabled: false },
      ...(affected.length ? [{ matchPackageNames: affected, enabled: true, minimumReleaseAge: null }] : []),
      // A security lookup must not re-enable duplicate managers or format-only pins.
      ...config.packageRules.filter(rule => rule.enabled === false),
      { matchPackageNames: ["github.com/misaon/nylzo", "ghcr.io/misaon/nylzo"], enabled: false },
    ] : config.packageRules), ...batchRules, ...isolationRules],
  },
};
