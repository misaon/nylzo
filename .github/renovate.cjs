// This global configuration is loaded only from protected main, never a PR.
const fs = require("node:fs");
const assert = require("node:assert/strict");
const config = JSON.parse(fs.readFileSync(process.env.NYLZO_RENOVATE_CONFIG || "renovate.json", "utf8"));
const mode = process.env.NYLZO_UPDATE_MODE;
assert(["security", "weekly"].includes(mode));
const tag = process.env.NYLZO_BASE_TAG;
assert(/^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(tag));
const affected = JSON.parse(process.env.NYLZO_AFFECTED_DEPENDENCIES || "[]");
assert(Array.isArray(affected) && affected.every(name => typeof name === "string" && /^[A-Za-z0-9@_./+-]+$/.test(name)));
module.exports = {
  ...config,
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
  allowedUnsafeExecutions: ["mise"],
  allowedCommands: ["^node /opt/nylzo-dependency-sync\\.mts --locks$"],
  allowShellExecutorForPostUpgradeCommands: false,
  cacheDir: "/tmp/renovate-cache",
  baseDir: "/tmp/renovate-work",
  persistRepoData: false,
  force: {
    ignoreScripts: true,
    ignorePlugins: true,
    automerge: false,
    platformCommit: "enabled",
    schedule: [],
    labels: ["dependencies", `automation:${mode}`],
    packageRules: mode === "security" ? [
      ...config.packageRules,
      { matchPackageNames: ["*"], enabled: false },
      ...(affected.length ? [{ matchPackageNames: affected, enabled: true, minimumReleaseAge: null }] : []),
      { matchPackageNames: ["github.com/misaon/nylzo", "ghcr.io/misaon/nylzo"], enabled: false },
    ] : config.packageRules,
  },
};
