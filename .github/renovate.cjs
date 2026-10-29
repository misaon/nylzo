// This global configuration is loaded only from protected main, never a PR.
const fs = require("node:fs");
const assert = require("node:assert/strict");
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
module.exports = {
  ...config,
  // Current manual holds do not consume the two ordinary update slots.
  prConcurrentLimit: config.prConcurrentLimit + blocked,
  platform: "github",
  repositories: ["misaon/nylzo"],
  autodiscover: false,
  onboarding: false,
  requireConfig: "ignored",
  baseBranchPatterns: [`release/${tag}`, "main"],
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
  // Native advisory remediations otherwise reset grouping and branchTopic.
  vulnerabilityAlerts: {
    ...config.vulnerabilityAlerts,
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
    ] : config.packageRules), ...batchRules],
  },
};
