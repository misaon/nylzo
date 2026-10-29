import { releaseBranch, trustedDependencyBot } from "../release/policy.mts";
import { repository } from "../release/github.mts";

export function blockedProposals(pulls: any[], tag: string): number {
  const current = releaseBranch(tag);
  return pulls.filter(pull => trustedDependencyBot(pull.user) && pull.head.repo.full_name === repository &&
    (pull.base.ref === "main" || pull.base.ref === current) &&
    pull.labels.some((label: any) => label.name === "requires-attention")).length;
}
