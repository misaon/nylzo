import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

export type BuildInfo = { version: string; commit: string; date: string; dirty: boolean };

const root = fileURLToPath(new URL("../", import.meta.url));
const number = "(?:0|[1-9]\\d*)";
const identifier = "(?:0|[1-9]\\d*|\\d*[A-Za-z-][0-9A-Za-z-]*)";
const semanticVersion = new RegExp(
  `^${number}\\.${number}\\.${number}(?:-${identifier}(?:\\.${identifier})*)?(?:\\+[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?$`,
);

export function buildInfo(directory = root, requestedVersion = process.env["NYLZO_VERSION"]): BuildInfo {
  function git(...arguments_: string[]): string {
    return execFileSync("git", arguments_, {
      cwd: directory,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    }).trim();
  }

  let commit = "";
  let date = "";
  let dirty = true;
  let tag = "";
  try {
    commit = git("rev-parse", "HEAD");
    date = new Date(git("show", "-s", "--format=%cI", "HEAD")).toISOString().replace(".000Z", "Z");
    dirty = git("status", "--porcelain", "--untracked-files=normal") !== "";
    try {
      tag = git("describe", "--exact-match", "--tags", "HEAD").replace(/^v/, "");
    } catch {
      tag = "";
    }
  } catch {
    commit = "";
    date = "";
  }
  const development = `0.0.0-dev${commit ? `.g${commit.slice(0, 12)}` : ""}${dirty ? ".dirty" : ""}`;
  const version = requestedVersion?.replace(/^v/, "") || (!dirty && semanticVersion.test(tag) ? tag : development);
  if (!semanticVersion.test(version)) {
    throw new Error("NYLZO_VERSION must be a semantic version");
  }
  return { version, commit, date, dirty };
}
