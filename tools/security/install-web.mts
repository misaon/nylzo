import { spawnSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { api, inspectUpdate } from "./dependency-api.mts";
import { repository } from "./dependency-policy.mts";

const directory = fileURLToPath(new URL("../../web/", import.meta.url));
const workspace = `${directory}/pnpm-workspace.yaml`;
const original = readFileSync(workspace, "utf8");
try {
  if (process.env.GITHUB_ACTIONS === "true" && process.env.GITHUB_REPOSITORY === repository && process.env.GH_TOKEN) {
    try {
      const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH!, "utf8"));
      const pull = event.pull_request ?? api(`repos/${repository}/commits/${process.env.GITHUB_SHA}/pulls`).find((candidate: any) => candidate.merged_at);
      if (pull) {
        const update = inspectUpdate(pull.number, Boolean(pull.merged_at));
        if (update.ecosystem === "npm") {
          if (/^minimumReleaseAgeExclude:/mu.test(original)) throw new Error("An existing age exception needs maintainer review");
          console.log(`Age exception for ${update.name}@${update.next}; fixes ${update.fixed.join(", ")}`);
          writeFileSync(workspace, `${original}\nminimumReleaseAgeExclude:\n  - ${JSON.stringify(`${update.name}@${update.next}`)}\n`);
        }
      }
    } catch {
      console.log("No verified security age exception; the normal release-age policy applies.");
    }
  }
  const result = spawnSync("pnpm", ["--dir", directory, "install", "--frozen-lockfile", "--config.confirmModulesPurge=false"], { stdio: "inherit" });
  process.exitCode = result.status ?? 1;
} finally {
  writeFileSync(workspace, original);
}
