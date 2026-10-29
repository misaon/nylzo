import { inspectDependencyPull } from "./inspect.mts";
import { pages } from "../release/github.mts";
import { fixedAdvisories } from "../security/dependency-policy.mts";

export function securityAgeExceptions(pull: any): string[] {
  const update = inspectDependencyPull(pull.number, Boolean(pull.merged_at));
  const exceptions = new Set<string>();
  for (const change of update.changes) {
    if (change.before === change.after) continue;
    const advisories = (version: string) => pages(`advisories?type=reviewed&ecosystem=npm&affects=${encodeURIComponent(`${change.name}@${version}`)}&per_page=100`);
    const fixed = fixedAdvisories({ name: change.name, previous: change.before, next: change.after, ecosystem: "npm" } as any, advisories(change.before), advisories(change.after));
    if (fixed.length) exceptions.add(`${change.name}@${change.after}`);
  }
  return [...exceptions];
}
