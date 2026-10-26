import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";

export function normalizeReport(report: string, project: string) {
  if (project !== "web/core" && project !== "web/app") throw new Error("Unknown coverage project");
  const root = process.cwd();
  return report.replace(/^SF:(.+)$/gmu, (_, source: string) => {
    const filename = path
      .relative(root, path.resolve(root, project, source))
      .split(path.sep)
      .join("/");
    if (!filename.startsWith(`${project}/src/`)) throw new Error(`Coverage path outside project sources: ${source}`);
    return `SF:${filename}`;
  });
}

if (import.meta.main) {
  const [project, input, output] = process.argv.slice(2);
  if (!project || !input || !output) throw new Error("Usage: normalize.mts web/core|web/app INPUT OUTPUT");
  writeFileSync(output, normalizeReport(readFileSync(input, "utf8"), project));
}
