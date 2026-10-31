import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { verifyDeepmergePatch } from "../sca/deepmerge.mts";

const directory = fileURLToPath(new URL("../../web/", import.meta.url));
const result = spawnSync("pnpm", ["--dir", directory, "install", "--frozen-lockfile", "--config.confirmModulesPurge=false"], { stdio: "inherit" });
process.exitCode = result.status ?? 1;
if (result.status === 0) verifyDeepmergePatch();
