import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";

export const repository = "misaon/nylzo";
export function request(path: string, method = "GET", body?: object): any {
  assert(!path.startsWith("/") && !path.includes(".."));
  const args = ["api", "--hostname", "github.com", path, "--method", method];
  if (body) args.push("--input", "-");
  const result = execFileSync("gh", args, { encoding: "utf8", maxBuffer: 16 * 1024 * 1024, ...(body ? { input: JSON.stringify(body) } : {}) });
  return result.trim() ? JSON.parse(result) : undefined;
}
export function pages(path: string): any[] {
  return JSON.parse(execFileSync("gh", ["api", "--hostname", "github.com", "--paginate", "--slurp", path], { encoding: "utf8", maxBuffer: 16 * 1024 * 1024 })).flat();
}
export function contents(path: string, sha: string): string {
  assert.match(sha, /^[a-f0-9]{40}$/u);
  const file = request(`repos/${repository}/contents/${path}?ref=${sha}`);
  assert.equal(file.type, "file"); assert.equal(file.encoding, "base64");
  return Buffer.from(file.content, "base64").toString("utf8");
}
export const git = (...args: string[]) => execFileSync("git", args, { encoding: "utf8" }).trim();
