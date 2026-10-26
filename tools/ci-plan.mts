import { appendFileSync } from "node:fs";

export function planChecks(files: string[], event: string) {
  if (!Array.isArray(files) || !files.every((file) => typeof file === "string" && file.length > 0)) {
    throw new Error("Changed files must be a JSON array of paths");
  }
  if (!["pull_request", "push", "schedule", "workflow_dispatch"].includes(event)) {
    throw new Error(`Unsupported CI event: ${event}`);
  }
  const groups = { go: false, web: false, integration: false, repository: false, dependencies: false, actions: false };
  let full = event === "schedule" || event === "workflow_dispatch" || files.length === 0;
  for (const file of files) {
    if (
      file === "docs/wiki/reference/Protocol.md" ||
      /^(internal\/(core|pepper)\/|cmd\/nylzo-vectors\/|spec\/|web\/core\/)/u.test(file)
    ) {
      full = true;
    } else if (
      /^(docs\/wiki\/.*\.md|README\.md|SECURITY\.md|CONTRIBUTING\.md|CODE_OF_CONDUCT\.md|LICENSE)$/u.test(file) ||
      /^(assets\/readme\/|skills\/.*\.md$|\.github\/(ISSUE_TEMPLATE\/|PULL_REQUEST_TEMPLATE\.md$))/u.test(file)
    ) {
      continue;
    } else if (
      /^(go\.(mod|sum)|mise\.(toml|lock)|\.goreleaser\.yaml|\.golangci\.yml)$/u.test(file) ||
      /^(\.github\/workflows\/|tools\/|mise-tasks\/)/u.test(file) ||
      /^web\/(.*\/)?(package\.json|pnpm-lock\.yaml|pnpm-workspace\.yaml|\.node-version)$/u.test(file)
    ) {
      full = true;
    } else if (/^(cmd\/|internal\/)/u.test(file) && !file.startsWith("internal/server/web/")) {
      groups.go = true;
      groups.integration = true;
    } else if (/^(web\/|internal\/server\/web\/|assets\/brand\/)/u.test(file)) {
      groups.web = true;
      groups.integration = true;
    } else if (file.startsWith("deploy/")) {
      groups.integration = true;
      groups.repository = true;
    } else {
      full = true;
    }
  }
  if (full) for (const group of Object.keys(groups) as (keyof typeof groups)[]) groups[group] = true;
  const jobs = {
    "go-lint": groups.go,
    "go-test": groups.go,
    "go-timing": groups.go,
    fuzz: groups.go,
    web: groups.web,
    coverage: groups.go || groups.web,
    repository: groups.repository,
    "dependency-review": event === "pull_request" && groups.dependencies,
    e2e: groups.integration,
    "dev-setup": groups.integration,
    compose: groups.integration,
    build: groups.integration,
    reproducible: groups.integration,
    "codeql-go": event === "pull_request" || groups.go,
    "codeql-web": event === "pull_request" || groups.web,
    "codeql-actions": event === "pull_request" || groups.actions,
  };
  return { ...groups, full, jobs };
}

export function checksPassed(results: Record<string, { result: string; outputs?: Record<string, string> }>) {
  const changes = results.changes;
  if (changes?.result !== "success") return false;
  let jobs: Record<string, boolean>;
  try {
    jobs = JSON.parse(changes.outputs?.jobs ?? "");
  } catch {
    return false;
  }
  if (!jobs || Array.isArray(jobs) || typeof jobs !== "object" || Object.keys(jobs).length === 0) return false;
  if (Object.keys(results).length !== Object.keys(jobs).length + 1) return false;
  return Object.entries(jobs).every(
    ([job, required]) => typeof required === "boolean" && results[job]?.result === (required ? "success" : "skipped"),
  );
}

if (import.meta.main) {
  if (process.argv[2] === "check") {
    if (!checksPassed(JSON.parse(process.env.RESULTS ?? ""))) {
      throw new Error("CI failed: a planned job failed, was cancelled, is missing, or was unexpectedly skipped");
    }
  } else {
    const event = process.env.EVENT ?? "";
    const files =
      event === "schedule" || event === "workflow_dispatch" ? [] : JSON.parse(process.env.CHANGED_FILES ?? "");
    const plan = planChecks(files, event);
    const output = Object.entries(plan)
      .map(([key, value]) => `${key}=${JSON.stringify(value)}\n`)
      .join("");
    appendFileSync(process.env.GITHUB_OUTPUT!, output);
    appendFileSync(
      process.env.GITHUB_STEP_SUMMARY!,
      `## Selected checks\n\nFull verification: **${plan.full}**\n\n| Job | Run |\n| --- | --- |\n` +
        Object.entries(plan.jobs)
          .map(([job, required]) => `| ${job} | ${required ? "yes" : "skip"} |\n`)
          .join(""),
    );
  }
}
