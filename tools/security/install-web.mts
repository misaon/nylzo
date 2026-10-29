import { spawnSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { api, inspectUpdate } from "./dependency-api.mts";
import { repository } from "./dependency-policy.mts";
import { verifyDeepmergePatch } from "../sca/deepmerge.mts";
import { trustedDependencyBot } from "../release/policy.mts";
import { securityAgeExceptions } from "../dependencies/age.mts";

const directory = fileURLToPath(new URL("../../web/", import.meta.url));
const workspace = `${directory}/pnpm-workspace.yaml`;
const original = readFileSync(workspace, "utf8");
try {
  if (process.env.GITHUB_ACTIONS === "true" && process.env.GITHUB_REPOSITORY === repository && process.env.GH_TOKEN) {
    try {
      const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH!, "utf8"));
      const pull = event.pull_request ?? api(`repos/${repository}/commits/${process.env.GITHUB_SHA}/pulls`).find((candidate: any) => candidate.merged_at);
      if (pull) {
        const exceptions: string[] = [];
        if (trustedDependencyBot(pull.user)) exceptions.push(...securityAgeExceptions(pull));
        else {
          const update = inspectUpdate(pull.number, Boolean(pull.merged_at));
          if (update.ecosystem === "npm") exceptions.push(`${update.name}@${update.next}`);
        }
        if (exceptions.length) {
          if (/^minimumReleaseAgeExclude:/mu.test(original)) throw new Error("An existing age exception needs maintainer review");
          console.log(`Exact release-age exceptions for ${exceptions.length} independently verified security fixes.`);
          writeFileSync(workspace, `${original}\nminimumReleaseAgeExclude:\n${exceptions.map(value => `  - ${JSON.stringify(value)}\n`).join("")}`);
        }
      }
    } catch {
      console.log("No verified security age exception; the normal release-age policy applies.");
    }
  }
  const result = spawnSync("pnpm", ["--dir", directory, "install", "--frozen-lockfile", "--config.confirmModulesPurge=false"], { stdio: "inherit" });
  process.exitCode = result.status ?? 1;
  if (result.status === 0) verifyDeepmergePatch();
} finally {
  writeFileSync(workspace, original);
}
