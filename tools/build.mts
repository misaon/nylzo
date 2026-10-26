import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { buildInfo } from "./build-info.mts";

const root = fileURLToPath(new URL("../", import.meta.url));
const output = resolve(root, process.argv[2] ?? "bin/nylzo");
const information = buildInfo();
const flags = Object.entries(information)
  .map(([name, value]) => `-X main.${name}=${value}`)
  .join(" ");
mkdirSync(dirname(output), { recursive: true });
execFileSync(process.env["NYLZO_GO"] ?? "go", ["build", "-trimpath", "-ldflags", flags, "-o", output, "./cmd/nylzo"], {
  cwd: root,
  stdio: "inherit",
  env: { ...process.env, CGO_ENABLED: "0" },
});
