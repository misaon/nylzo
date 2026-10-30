import { trustedDependencyBot } from "../release/policy.mts";
import { repository } from "../release/github.mts";

export function blockedProposals(pulls: any[]): number {
  return pulls.filter(pull => pull.state === "open" && trustedDependencyBot(pull.user) && pull.head.repo.full_name === repository &&
    pull.labels.some((label: any) => label.name === "requires-attention" || label.name === "automation:diagnostic-passed")).length;
}
